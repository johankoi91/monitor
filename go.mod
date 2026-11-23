module github.com/johankoi91/monitor/runtime

go 1.21

require (
	github.com/gorilla/websocket v1.5.3
	github.com/lib/pq v1.10.9
	golang.org/x/crypto v0.24.0
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2 => ./probe/third_party/Agora-Golang-Server-SDK-2.6.6

require github.com/AgoraIO/Tools/DynamicKey/AgoraDynamicKey/go/src v0.0.0-20240807100336-95d820182fef

require github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2 v2.6.6
