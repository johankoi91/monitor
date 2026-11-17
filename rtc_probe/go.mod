module github.com/johankoi91/monitor/rtc-probe

go 1.21

require github.com/gorilla/websocket v1.5.3

require (
	github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2 v2.6.6
	github.com/AgoraIO/Tools/DynamicKey/AgoraDynamicKey/go/src v0.0.0-20240807100336-95d820182fef
)

replace github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2 => ../Agora-Golang-Server-SDK-2.6.6
