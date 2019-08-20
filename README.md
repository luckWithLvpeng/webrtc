# 推流demo
1. 进入channel， yarn install 然后 启动node
2. 进入clientgo, 启动 go客户端
3. 进入webrtc-browser, yarn install, 然后 yarn start 启动网页服务。

------------

打开网页localhost:3000, 点击推流按钮， go客户端接受流，会保存到output-ID.ivf文件， 通过 ll -h ,可以看到文件的大小一直在涨。后台同时保存了这个流。
再开一个新网页localhost:3000， 点 拉流，会播放上步推送的视频流。
可以多次，打开新的网页，点 拉流， 可以同步播放多个窗口。实现一对多。

再打开网页，点击播放视频 按钮，则可以播放之前缓存的视频。
