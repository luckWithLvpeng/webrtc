package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	gosocketio "github.com/graarh/golang-socketio"

	"clientgo/ivfreader"
	"clientgo/ivfwriter"

	"github.com/graarh/golang-socketio/transport"
	"github.com/nareix/joy4/av/avutil"
	"github.com/nareix/joy4/av/pubsub"
	"github.com/nareix/joy4/format/rtmp"
	"github.com/pion/rtcp"
	webrtc "github.com/pion/webrtc/v2"
	media "github.com/pion/webrtc/v2/pkg/media"
)

//Message 客户端发送和接收的信息格式
//
//  From 客户端的socket.ID
//  To  server的Mac 地址
//  Sdp  base64 编码的sdp, 需要加密
//  Type  消息的类型
//  Msg  当消息类型为错误的时候，附带的信息
//  Candidate  candidate 验证参数
//  SDPMid  candidate 验证参数
//  SDPMLineIndex  candidate 验证参数
//  UsernameFragment  candidate 验证参数
type Message struct {
	From             string `json:"from"`
	To               string `json:"to"`
	Sdp              string `json:"sdp"`
	Type             string `json:"type"`
	Msg              string `json:"msg"`
	Candidate        string `json:"candidate"`
	SDPMid           string `json:"sdpMid"`
	SDPMLineIndex    uint16 `json:"sdpMLineIndex"`
	UsernameFragment string `json:"usernameFragment"`
}

var (
	client             *gosocketio.Client
	webURL             = gosocketio.GetUrl("127.0.0.1", 10900, false)
	websocketTransport = &transport.WebsocketTransport{
		PingInterval:   10 * time.Second,
		PingTimeout:    30 * time.Second,
		ReceiveTimeout: 30 * time.Second,
		SendTimeout:    30 * time.Second,
		BufferSize:     1024 * 32,
	}
	err    error
	pcs    = make(map[string]*webrtc.PeerConnection)
	config = webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			// {
			// 	URLs:           []string{"turn:127.0.0.1:3478"},
			// 	Username:       "username",
			// 	Credential:     "password",
			// 	CredentialType: webrtc.ICECredentialTypePassword,
			// },
		},
	}
	tracks        = make(map[int]*webrtc.Track)
	pipelinesLock sync.Mutex
	// 服务器ID
	mac               = "123"
	pcsLock           sync.Mutex
	ClientsMap        = make(map[int][]string) //key 为channel id  value 为客户端id字符串数组
	clientsMapLock    sync.Mutex
	clientsStatus     = make(map[string]int) //key 为clientid
	clientsStatusLock sync.Mutex
	createRoomTimer   *time.Timer
	VideoTrack        *webrtc.Track
	ivfFile           *ivfwriter.IVFWriter
)

const (
	videoClockRate = 90000
	audioClockRate = 48000
)

func createOrJoinRoom() {
	client.Emit("createOrJoin", mac)
	createRoomTimer = time.AfterFunc(time.Second*3, createOrJoinRoom)
}

//连接socketio 信令服务器
func connect() {
	client, err = gosocketio.Dial(webURL, websocketTransport)
	if err != nil {
		log.Println("链接", webURL, "失败, 请先启动channel服务, 2 秒后重连")
		// 连接失败后， 间隔一段时间后再次重连
		time.AfterFunc(time.Second*2, connect)
		return
	}
	client.On(gosocketio.OnDisconnection, func(h *gosocketio.Channel) {
		log.Println("Disconnected")
		// 停止创建房间
		createRoomTimer.Stop()
		connect()
	})
	client.On(gosocketio.OnError, func(err error) {
		log.Println("Webrtc gosocketio has err:", err)

	})
	client.On(gosocketio.OnConnection, func(h *gosocketio.Channel) {
		log.Println("Connected")
		// 创建房间， 失败后一段时间自动重新创建。
		createOrJoinRoom()
	})
	client.On("log", func(h *gosocketio.Channel, args []string) {
		//log.Println("log from server:", strings.Join(args, " "))
	})
	//建立连接后初始化通道控制
	client.On("created", func(h *gosocketio.Channel, room string) {
		log.Println("created room ", room)
		// 停止创建房间
		createRoomTimer.Stop()

	})
	// 客户端请求建立连接
	client.On("askToConnect", func(h *gosocketio.Channel, msg Message) {
		err := createPeerConnection(msg.From)
		if err != nil {
			client.Emit("messageToBrowser", Message{
				Type: "error",
				To:   msg.From,
				From: msg.To,
				Msg:  err.Error(),
			})
			return
		}
		client.Emit("messageToBrowser", Message{
			Type: "ready",
			To:   msg.From,
			From: msg.To,
			Msg:  "OK",
		})

	})
	client.On("messageToDevice", func(h *gosocketio.Channel, msg Message) {
		pcsLock.Lock()
		pc := pcs[msg.From]
		pcsLock.Unlock()
		if pc != nil {
			if msg.Type == "offer" {
				offer := webrtc.SessionDescription{}
				tmpbyte, err := base64.StdEncoding.DecodeString(msg.Sdp)
				defer func() {
					if e := recover(); e != nil {
						client.Emit("messageToBrowser", Message{
							Type: "error",
							To:   msg.From,
							From: msg.To,
							Msg:  fmt.Sprintf("run time panic: %v", e),
						})
						return
					}
				}()
				if err != nil {
					client.Emit("messageToBrowser", Message{
						Type: "error",
						To:   msg.From,
						From: msg.To,
						Msg:  err.Error(),
					})
					return
				}
				json.Unmarshal(tmpbyte, &offer)
				err = pc.SetRemoteDescription(offer)
				if err != nil {
					client.Emit("messageToBrowser", Message{
						Type: "error",
						To:   msg.From,
						From: msg.To,
						Msg:  err.Error(),
					})
					return
				}
				answer, err := pc.CreateAnswer(nil)
				if err != nil {
					client.Emit("messageToBrowser", Message{
						Type: "error",
						To:   msg.From,
						From: msg.To,
						Msg:  err.Error(),
					})
					return
				}
				err = pc.SetLocalDescription(answer)
				if err != nil {
					client.Emit("messageToBrowser", Message{
						Type: "error",
						To:   msg.From,
						From: msg.To,
						Msg:  err.Error(),
					})
					return
				}
				tmpbyte, err = json.Marshal(answer)
				if err != nil {
					client.Emit("messageToBrowser", Message{
						Type: "error",
						To:   msg.From,
						From: msg.To,
						Msg:  err.Error(),
					})
					return
				}
				client.Emit("messageToBrowser", Message{
					Type: "answer",
					To:   msg.From,
					From: msg.To,
					Sdp:  base64.StdEncoding.EncodeToString(tmpbyte),
				})
			} else if msg.Type == "candidate" {
				pc.AddICECandidate(webrtc.ICECandidateInit{
					Candidate:        msg.Candidate,
					SDPMid:           &msg.SDPMid,
					SDPMLineIndex:    &msg.SDPMLineIndex,
					UsernameFragment: msg.UsernameFragment,
				})
			}
		}

	})
}

func createPeerConnection(clientID string) error {
	if pcs[clientID] == nil {
		// 创建 pc
		peerConnection, err := webrtc.NewPeerConnection(config)
		if err != nil {
			return err
		}
		peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
			//打印 ice 状态的变化
			log.Println(fmt.Sprintf("Connection State has changed %s", connectionState.String()))
			switch connectionState.String() {
			case "connected":
			// case "failed":
			// callClientsUseRtmpById()
			case "disconnected":
				peerConnection.Close()
			}
			// // 当远程pc 失去连接 ,关闭本地客户端，清除数据
			// if connectionState.String() == "disconnected" {
			// 	peerConnection.Close()
			// }
		})
		peerConnection.OnICECandidate(func(ICECandidate *webrtc.ICECandidate) {

			//client.Emit("messageToBrowser",Message{
			//	Type: "candidate",
			//	Candidate:        ICECandidate.Candidate,
			//	SDPMid:           ICECandidate.SDPMid,
			//	SDPMLineIndex:    ICECandidate.SDPMLineIndex,
			//	UsernameFragment: ICECandidate.UsernameFragment,
			//})
		})
		peerConnection.OnTrack(func(track *webrtc.Track, receiver *webrtc.RTPReceiver) {
			// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
			fmt.Println("111")
			go func() {
				ticker := time.NewTicker(time.Second * 3)
				for range ticker.C {
					errSend := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: track.SSRC()}})
					if errSend != nil {
						fmt.Println(errSend)
					}
				}
			}()

			codec := track.Codec()
			if codec.Name == webrtc.VP8 {
				fmt.Println("Got VP8 track, saving to disk as output.ivf")
				saveToDisk(ivfFile, track)
			}
		})
		addStream(peerConnection, clientID)
		pcsLock.Lock()
		pcs[clientID] = peerConnection
		pcsLock.Unlock()
		return err
	}
	return nil
}

func saveToDisk(i media.Writer, track *webrtc.Track) {
	defer func() {
		if err := i.Close(); err != nil {
			panic(err)
		}
	}()

	for {
		rtpPacket, err := track.ReadRTP()
		if err != nil {
			panic(err)
		}
		if err := i.WriteRTP(rtpPacket); err != nil {
			panic(err)
		}
	}
}

func addStream(peerConnection *webrtc.PeerConnection, clientID string) {
	// 没有则先创建这个通道视频
	if VideoTrack == nil {
		VideoTrack, err = peerConnection.NewTrack(webrtc.DefaultPayloadTypeVP8, rand.Uint32(), mac, mac)
		go playVideo()
		//go listenRTMPStream()
		if err != nil {
			sendErrorToClient(err, clientID)
		}
	}
	_, err = peerConnection.AddTrack(VideoTrack)
	if err != nil {
		sendErrorToClient(err, clientID)
	}
}

func sendErrorToClient(err error, clientID string) {
	client.Emit("messageToBrowser", Message{
		Type: "error",
		To:   clientID,
		From: mac,
		Msg:  err.Error(),
	})
}

func playVideo() {
	// Open a IVF file and start reading using our IVFReader
	file, ivfErr := os.Open("test.ivf")
	if ivfErr != nil {
		panic(ivfErr)
	}

	ivf, header, ivfErr := ivfreader.NewWith(file)
	if ivfErr != nil {
		panic(ivfErr)
	}

	// Send our video file frame at a time. Pace our sending so we send it at the same speed it should be played back as.
	// This isn't required since the video is timestamped, but we will such much higher loss if we send all at once.
	sleepTime := time.Millisecond * time.Duration((float32(header.TimebaseNumerator)/float32(header.TimebaseDenominator))*1000)
	for {
		frame, _, ivfErr := ivf.ParseNextFrame()
		if ivfErr != nil {
			panic(ivfErr)
		}

		time.Sleep(sleepTime)
		if ivfErr = VideoTrack.WriteSample(media.Sample{Data: frame, Samples: 90000}); ivfErr != nil {
			panic(ivfErr)
		}
	}
}

func listenRTMPStream() {
	server := &rtmp.Server{}
	l := &sync.RWMutex{}
	type Channel struct {
		que *pubsub.Queue
	}
	channels := map[string]*Channel{}
	server.HandlePlay = func(conn *rtmp.Conn) {
		l.RLock()
		ch := channels[conn.URL.Path]
		l.RUnlock()
		if ch != nil {
			cursor := ch.que.Latest()
			avutil.CopyFile(conn, cursor)
		}
	}

	server.HandlePublish = func(conn *rtmp.Conn) {
		streams, _ := conn.Streams()

		l.Lock()
		fmt.Println("request string->", conn.URL.RequestURI())
		fmt.Println("request key->", conn.URL.Query().Get("key"))
		ch := channels[conn.URL.Path]
		if ch == nil {
			ch = &Channel{}
			ch.que = pubsub.NewQueue()
			ch.que.WriteHeader(streams)
			channels[conn.URL.Path] = ch
		} else {
			ch = nil
		}
		l.Unlock()
		if ch == nil {
			return
		}

		avutil.CopyPackets(ch.que, conn)

		l.Lock()
		delete(channels, conn.URL.Path)
		l.Unlock()
		ch.que.Close()
	}

}

func main() {

	ivfFile, _ = ivfwriter.New("output.ivf")
	// 启动wertc
	if true {
		connect()
	}

	go listenRTMPStream()
	for true {
		time.Sleep(time.Hour * 10)
	}

}
