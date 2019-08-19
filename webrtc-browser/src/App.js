import React, {Component} from 'react';
import './App.css';
import io from 'socket.io-client'
import 'webrtc-adapter'
import 'toastr/build/toastr.min.css'
import toastr from 'toastr'
// import ReactPlayer from 'react-player'
// import VideoPlayer from 'react-videojs-wrapper';
// import {ReactFlvPlayer} from 'react-flv-player'
// import flvjs from 'flv.js'

var pcConfig = {
    'iceServers': [
        {
            'urls': ['stun:127.0.0.1:3478'],
        },
        {
            'urls': ['turn:127.0.0.1:3478'],
            'credentialType': 'password',
            'credential': 'username',
            'username': 'password'
        }
    ],
    // "iceTransportPolicy": "relay"
};

class App extends Component {
    state = {
        status: "正在链接 server  ... ",
        ready: false,
        pcs: {},
        mac: "",
        sdp: "",
        localStream: null,
        type: ""

    }

    handleIceCandidate(event, macAddr) {
        // console.log('icecandidate event: ', event);
        if (event.candidate) {
            this.sendMessage({
                type: 'candidate',
                sdpMid: event.candidate.sdpMid,
                candidate: event.candidate.candidate,
                sdpMLineIndex: event.candidate.sdpMLineIndex,
                usernameFragment: event.candidate.usernameFragment,
                from: this.socket.id, // 本地链接的id
                to: macAddr,// 要连接的盒子的mac地址
            });
        } else {
            console.log('End of candidates.');
        }
    }

    sendMessage(message) {
        console.log('Client sending message: ', message);
        this.socket.emit('messageToDevice', message);
    }


    onTrack(event) {
        console.log("add stream")
        var el = document.createElement(event.track.kind)
        el.srcObject = event.streams[0]
        el.autoplay = true
        el.controls = true
        document.getElementById('remoteVideos').appendChild(el)
    }


    creatPeerConnection(macAddr) {
        var pc = new RTCPeerConnection(pcConfig);
        var self = this;

        // 创建offer 准备发给盒子
        pc.onicecandidate = (e) => {
            self.handleIceCandidate.bind(self)(e, macAddr)
        };
        pc.oniceconnectionstatechange = (e) => {
            // console.log(pc)
            // console.log(pc.iceConnectionState())
            document.querySelector("div#status").innerHTML += pc.iceConnectionState + "<br>"
        };
        if (this.state.action !== "push to file") {
            pc.ontrack = this.onTrack.bind(this);
        }
        if (this.state.localStream) {
            pc.addStream(this.state.localStream)
        }
        // 准备接收一路视频
        pc.addTransceiver('video', {'direction': 'recvonly'})
        this.doCall(pc, macAddr)
        this.setState({
            pcs: {
                ...this.state.pcs,
                [macAddr]: pc
            }
        })
    }

    doCall(pc, macAddr) {
        pc.createOffer({offerToReceiveVideo: true}).then((sdp) => {
            pc.setLocalDescription(sdp)
            this.sendMessage({
                from: this.socket.id,
                to: macAddr,
                sdp: btoa(JSON.stringify(sdp)),
                type: "offer"
            })
        }).catch((err) => {
            console.log(err)
        })
    }

    componentDidMount() {

        var self = this;



        this.socket = io('ws://127.0.0.1:10900');
        //
        this.socket.on('connect', function () {
            self.setState({status: "链接服务成功"})
            self.setState({ready: true})
        });
        this.socket.on("messageToBrowser", function (message) {
            console.log("client recive message", message)
            if (message.type === "ready") { // 等待服务区发来ready 消息
                self.creatPeerConnection(message.from)
            } else if (message.type === "answer") {
                if (self.state.pcs[message.from]) {
                    console.log("answer", JSON.parse(atob(message.sdp)))
                    self.state.pcs[message.from].setRemoteDescription(new RTCSessionDescription(JSON.parse(atob(message.sdp))))
                }
            } else if (message.type === "error") {
                toastr.error(message.msg)
            }

        })
        this.socket.on("error", function (res) {
            console.log(res)
        })
        this.socket.on("log", function (array) {
            console.log.apply(console, array)
        })
        this.socket.on('disconnect', function () {
            self.setState({status: "失去连接,正在尝试重连..."})
            self.setState({ready: false})
        });
    }

    startConnect(action) {
        var mac = prompt("请输入 mac : 123")
        if (mac !== null && mac.trim() !== "") {
            // 发送请求，是否可以连接上视频服务
            this.setState({mac: mac, action: action})
            this.socket.emit("canConnect", {to: mac, from: this.socket.id,msg:action})

        } else {
            // alert("请输入服务器 mac 地址")
        }
    }

    render() {
        return (
            <div className="App">
                <h1>推流demo </h1>
                <aside>{this.state.status}</aside>
                <aside>{this.state.room}</aside>
                {
                    !this.state.action ? (
                        <div>
                            <br/>
                            <br/>
                            <br/>
                            <button disabled={!this.state.ready} onClick={() => {
                                var self = this
                                navigator.mediaDevices.getUserMedia({video: true})
                                    .then(stream => {
                                        document.getElementById('video').srcObject = stream
                                        self.setState({localStream: stream})
                                        self.startConnect("push to file")

                                    })

                            }}> 推流并保存文件->
                                output.ivf
                            </button>
                            <br/>
                            <br/>
                            <br/>
                            <button disabled={!this.state.ready} onClick={() => this.startConnect("pull from file")}> 播放视频文件</button>
                            <br/>
                            <br/>
                            <br/>
                            <button disabled={!this.state.ready} onClick={() => this.startConnect("pull from stream")}> 拉流</button>
                            <br/>
                            <br/>
                            <br/>
                        </div>
                    ): (
                        <h1>
                            {this.state.action}
                        </h1>
                    )
                }

                <div id={"status"}> </div>
                <div id={"remoteVideos"}>
                    <video id="video" width="400" height="300" autoPlay muted style={{margin: "auto"}}> </video>
                </div>
            </div>
        );
    }
}

export default App;
