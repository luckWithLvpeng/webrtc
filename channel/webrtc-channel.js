'use strict';

var os = require('os');
var fs = require('fs');
var nodeStatic = require('node-static');
var https = require('https');
var http = require('http');
var socketIO = require('socket.io');

var fileServer = new (nodeStatic.Server)();
var app = http.createServer(function(req, res) { fileServer.serve(req, res); })
              .listen(10900);
console.log("now server started at localhost:10900")
var io = socketIO.listen(app);
io.sockets.on('connection', function(socket) {

        // convenience function to log server messages on the client
        function log() {
                var array = ['log from server:'];
                array.push.apply(array, arguments);
                socket.emit('log', array);
        }

        socket.on('messageToBrowser', function(message) {
                //log('device said: ', message);
                socket.broadcast.to(message.to).emit('messageToBrowser', message);
        });
        socket.on('messageToDevice', function(message) {
                if (!message) {
                    return
                }
                //log('browser said: ', message);
                var clientsInRoom = io.sockets.adapter.rooms[message.to];
                if (clientsInRoom && clientsInRoom.length > 0) {
                        io.sockets.in (message.to).emit("messageToDevice", message);
                } else {
                        var tmp = message.from;
                        message.from = message.to;
                        message.to = tmp;
                        message.type = "error";
                        message.msg = "该设备失联，请稍后重试";
                        socket.emit("messageToBrowser", message);
                }
        });

        socket.on('canConnect', function(data, callBack) {
                if (!data) {
                    return
                }
                var clientsInRoom = io.sockets.adapter.rooms[data.to];
                if (clientsInRoom && clientsInRoom.length > 0) {
                        io.sockets.in (data.to).emit("askToConnect", data);
                }else {
                    if (typeof callBack === "function") {
                        callBack(new Error("服务失联，请稍后再试").toString());
                        return
                    }else {
                        socket.emit("messageToBrowser", {
                            type: "error",
                            from: data.to,
                            to: data.from,
                            msg: "服务失联，请稍后重试",
                        });
                    }
                }

        });
        socket.on('createOrJoin', function(room) {
                if (!room) {
                    return
                }
                log('Received request to create ' + room);
                socket.join(room);
                socket.emit('created', room);
        });

        // for test
        socket.on('bye', function() { console.log('received bye'); });

});

