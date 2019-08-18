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
    localStream: null

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

  handleRemoteStreamRemoved(event) {
    console.log('Remote stream removed. Event: ', event);
  }

  onTrack(event) {
    console.log("add stream")
    var el = document.createElement(event.track.kind)
    console.log(event.streams[0])
    el.srcObject = event.streams[0]
    el.autoplay = true
    el.controls = true
    document.getElementById('remoteVideos').appendChild(el)
  }

  // handleRemoteStreamAdded(event) {
  //     console.log('Remote stream added.');
  // }

  creatPeerConnection(macAddr)  {
    var pc = new RTCPeerConnection(pcConfig);
    var self = this;
    // 准备接收一路视频
    pc.addTransceiver('video', {'direction': 'recvonly'})
    // 创建offer 准备发给盒子
    pc.onicecandidate = (e) => {
      self.handleIceCandidate.bind(self)(e, macAddr)
    };
    pc.oniceconnectionstatechange = (e) => {
      // console.log(pc)
      // console.log(pc.iceConnectionState())
      document.querySelector("div#status").innerHTML += pc.iceConnectionState + "<br>"
    };
    // pc.onsignalingstatechange = (e) => {
    //     console.log("onsignalingstatechange",e)
    // };
    // pc.onicegatheringstatechange = (e) => {
    //     console.log("onicegatheringstatechange",e)
    // };
    // pc.onconnectionstatechange = (e) => {
    //     console.log("onconnectionstatechange",e)
    // };
    // pc.ondatachannel = (e) => {
    //     console.log("ondatachannel",e)
    // };
    // pc.onaddstream = this.handleRemoteStreamAdded.bind(this);
    // pc.onremovestream = this.handleRemoteStreamRemoved.bind(this);
    pc.ontrack = this.onTrack.bind(this);
    if (this.state.localStream) {
      pc.addStream(this.state.localStream)
    }
    this.doCall(pc,macAddr)
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
    // if (flvjs.isSupported()) {
    //   var videoElement = document.getElementById('player');
    //   var flvPlayer = flvjs.createPlayer({
    //     type: 'flv',
    //     url: 'http://175.25.168.16/pl3.live.panda.tv/live_panda/d4e0a83a7e0b0c6e4c5d03774169fa3e.flv?wshc_tag=0&wsts_tag=57e233b1&wsid_tag=6a27c14e&wsiphost=ipdbm'
    //   });
    //   flvPlayer.attachMediaElement(videoElement);
    //   flvPlayer.load();
    //   flvPlayer.play();
    // }

    var self = this;
    navigator.mediaDevices.getUserMedia({ video: true })
        .then(stream => {
          document.getElementById('video1').srcObject = stream
          this.setState({localStream: stream})
          // pc.addStream()
          // pc.createOffer().then(d => pc.setLocalDescription(d))
        })


    this.socket = io('ws://127.0.0.1:10900');
    //
    this.socket.on('connect', function () {
      self.setState({status: "链接成功"})
      self.setState({ready: true})
    });
    this.socket.on("messageToBrowser", function (message) {
      console.log("client recive message", message)
      if (message.type === "ready") { // 等待服务区发来ready 消息
        self.creatPeerConnection(message.from)
      } else if (message.type === "answer") {
        if (self.state.pcs[message.from]) {
          console.log("answer",JSON.parse(atob(message.sdp)))
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

  startConnect() {
    var mac = prompt("请输入 mac : 123")
    if (mac !== null && mac.trim() !== "") {
      // 发送请求，是否可以连接上视频服务
      this.setState({mac: mac})
      this.socket.emit("canConnect", {to: mac, from: this.socket.id})
      console.log("已发送")
    } else {
      // alert("请输入服务器 mac 地址")
    }
  }

  render() {
    return (
        <div className="App">
          {/*<ReactPlayer url='http://192.168.1.155:7002/live/movie.m3u8' playing />*/}
          {/*<ReactFlvPlayer*/}
          {/*    url = "http://192.168.1.155:7001/live/movie.flv"*/}
          {/*    heigh = "800px"*/}
          {/*    width = "800px"*/}
          {/*    isMuted={true}*/}
          {/*/>*/}

          <h1>推流demo </h1>
          <aside>{this.state.status}</aside>
          <aside>{this.state.room}</aside>
          <div>摄像头</div>
          <div>
            <video id="video1" width="400" height="300" autoPlay muted style={{margin:"auto"}}></video>
          </div>
          <br/><br/>
          <button disabled={!this.state.ready} onClick={() => this.startConnect()}> 推流并获取视频</button>
          <br/><br/><br/>
          <div id={"status"}></div>
          <div id={"remoteVideos"}></div>
        </div>
    );
  }
}

export default App;
