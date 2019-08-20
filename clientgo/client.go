package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	gosocketio "github.com/graarh/golang-socketio"

	"clientgo/ivfreader"
	"clientgo/ivfwriter"

	"github.com/graarh/golang-socketio/transport"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
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
	// 服务器ID
	mac             = "123"
	pcsLock         sync.Mutex
	createRoomTimer *time.Timer
	m               = webrtc.MediaEngine{}
	api             *webrtc.API
	localTrack      *webrtc.Track
	firstPush       bool
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
		err := createPeerConnection(msg.From, msg.Msg)
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

func createPeerConnection(clientID string, action string) error {
	if pcs[clientID] == nil {
		// 创建 pc
		//peerConnection, err := webrtc.NewPeerConnection(config)
		peerConnection, err := api.NewPeerConnection(config)
		if err != nil {
			return err
		}
		ivfFile, _ := ivfwriter.New("output-" + clientID + ".ivf")
		peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
			fmt.Printf("Connection State has changed %s \n", connectionState.String())

			if connectionState == webrtc.ICEConnectionStateConnected {
				fmt.Println("Ctrl+C the remote client to stop the demo")
			} else if connectionState == webrtc.ICEConnectionStateFailed ||
				connectionState == webrtc.ICEConnectionStateDisconnected {

				closeErr := ivfFile.Close()
				if closeErr != nil {
					panic(closeErr)

				}

				fmt.Println("Done writing media files")
				os.Exit(0)
			}
		})
		if action == "push to file and stream" {
			// Allow us to receive 1 audio track, and 1 video track
			if _, err = peerConnection.AddTransceiver(webrtc.RTPCodecTypeAudio); err != nil {
				panic(err)
			} else if _, err = peerConnection.AddTransceiver(webrtc.RTPCodecTypeVideo); err != nil {
				panic(err)
			}
			peerConnection.OnTrack(func(track *webrtc.Track, receiver *webrtc.RTPReceiver) {
				// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
				go func() {
					ticker := time.NewTicker(time.Second * 3)
					for range ticker.C {
						errSend := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: track.SSRC()}})
						if errSend != nil {
							fmt.Println(errSend)
						}
					}
				}()
				if localTrack == nil {
					localTrack, _ = peerConnection.NewTrack(track.PayloadType(), track.SSRC(), "video", "pion")
				}
				codec := track.Codec()
				if codec.Name == webrtc.VP8 {
					fmt.Println("Got VP8 track, saving to disk as output-" + clientID + ".ivf")
					saveToDiskAndAddtoLocaltrack(ivfFile, track)
				}
			})
		}
		//addStream(peerConnection, clientID)
		if action == "pull from stream" {
			fmt.Println("pull from stream")
			if err != nil {
				sendErrorToClient(err, clientID)
			}
			_, err = peerConnection.AddTrack(localTrack)
			if err != nil {
				sendErrorToClient(err, clientID)
			}

		}
		//addStream(peerConnection, clientID)
		if action == "pull from file" {
			fmt.Println("pull")
			VideoTrack, err := peerConnection.NewTrack(webrtc.DefaultPayloadTypeVP8, rand.Uint32(), mac, mac)
			if err != nil {
				sendErrorToClient(err, clientID)
			}
			go playVideo(VideoTrack)
			_, err = peerConnection.AddTrack(VideoTrack)
			if err != nil {
				sendErrorToClient(err, clientID)
			}

		}
		pcsLock.Lock()
		pcs[clientID] = peerConnection
		pcsLock.Unlock()
		return err
	}
	return nil
}

func saveToDiskAndAddtoLocaltrack(i media.Writer, track *webrtc.Track) {
	defer func() {
		if err := i.Close(); err != nil {
			//panic(err)
		}
	}()

	rtpBuf := make([]byte, 8192)
	for {
		n, err := track.Read(rtpBuf)
		if err != nil {
			fmt.Println("读取视频帧数据Error", err)
		}
		// ErrClosedPipe means we don't have any subscribers, this is ok if no peers have connected yet
		if _, err = localTrack.Write(rtpBuf[:n]); err != nil && err != io.ErrClosedPipe {
			//panic(err)
			fmt.Println("流分发出错Error", err)
		}
		rtpPacket := &rtp.Packet{}
		if err := rtpPacket.Unmarshal(rtpBuf[:n]); err != nil {
			fmt.Println("解析视频数据Error", err)
		}

		// rtpPacket, err := track.ReadRTP()
		// if err != nil {
		// 	panic(err)
		// }
		// if firstPush == false {
		// 	// 流分发
		// 	if err = localTrack.WriteRTP(rtpPacket); err != nil && err != io.ErrClosedPipe {
		// 		// panic(err)
		// 		fmt.Println("流分发出错", err)
		// 	}
		// 	firstPush = true
		// }
		// 保存视频文件
		if err := i.WriteRTP(rtpPacket); err != nil {
			//panic(err)
		}
	}
}

func addStream(peerConnection *webrtc.PeerConnection, clientID string) {
	// 没有则先创建这个通道视频
	VideoTrack, err := peerConnection.NewTrack(webrtc.DefaultPayloadTypeVP8, rand.Uint32(), mac, mac)
	if err != nil {
		sendErrorToClient(err, clientID)
	}
	go playVideo(VideoTrack)
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

func playVideo(VideoTrack *webrtc.Track) {
	// Open a IVF file and start reading using our IVFReader
	file, ivfErr := os.Open("test.ivf")
	if ivfErr != nil {
		//panic(ivfErr)
	}

	ivf, header, ivfErr := ivfreader.NewWith(file)
	if ivfErr != nil {
		//panic(ivfErr)
	}

	// Send our video file frame at a time. Pace our sending so we send it at the same speed it should be played back as.
	// This isn't required since the video is timestamped, but we will such much higher loss if we send all at once.
	sleepTime := time.Millisecond * time.Duration((float32(header.TimebaseNumerator)/float32(header.TimebaseDenominator))*1000)
	for {
		frame, _, ivfErr := ivf.ParseNextFrame()
		if ivfErr != nil {
			//panic(ivfErr)
		}

		time.Sleep(sleepTime)
		if ivfErr = VideoTrack.WriteSample(media.Sample{Data: frame, Samples: 90000}); ivfErr != nil {
			//panic(ivfErr)
		}
	}
}

func main() {

	// Setup the codecs you want to use.
	// We'll use a VP8 codec but you can also define your own
	m.RegisterCodec(webrtc.NewRTPOpusCodec(webrtc.DefaultPayloadTypeOpus, 48000))
	m.RegisterCodec(webrtc.NewRTPVP8Codec(webrtc.DefaultPayloadTypeVP8, 90000))

	// Create the API object with the MediaEngine
	api = webrtc.NewAPI(webrtc.WithMediaEngine(m))
	// 启动wertc
	if true {
		connect()
	}

	for true {
		time.Sleep(time.Hour * 10)
	}

}
