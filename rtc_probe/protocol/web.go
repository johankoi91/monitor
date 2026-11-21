package protocol

import "errors"

const WebDriver = "agora-web"
const WebMediaCapability = "web_audio_video_v1"

type WebAccessConfiguration struct {
	Port               int                   `json:"port"`
	ReportPort         int                   `json:"report_port"`
	LogPort            int                   `json:"log_port"`
	IgnoreCertificates bool                  `json:"ignore_certificates"`
	Coverage           *NativeCoverageConfig `json:"coverage,omitempty"`
}

func (c *WebAccessConfiguration) Normalize() error {
	if c.Port == 0 {
		c.Port = 443
	}
	if c.ReportPort == 0 {
		c.ReportPort = 6443
	}
	if c.LogPort == 0 {
		c.LogPort = 6444
	}
	for _, port := range []int{c.Port, c.ReportPort, c.LogPort} {
		if port < 1 || port > 65535 {
			return errors.New("Web 接入端口无效")
		}
	}
	if c.Coverage != nil {
		return c.Coverage.Normalize()
	}
	return nil
}
func RealDriver(driver string) bool { return driver == "agora" || driver == WebDriver }
func TaskDriver(task Task, target Target) string {
	if target.Driver == "agora" && task.ClientType == "web" {
		return WebDriver
	}
	return target.Driver
}

const InteropClient = "interop"
const InteropAUTClient = "interop_aut"

func IsInteropClient(client string) bool {
	return client == InteropClient || client == InteropAUTClient
}

func InteropNativeTransport(client string) string {
	if client == InteropAUTClient {
		return "aut"
	}
	return "udp"
}

func InteropCapability(client string) string {
	if client == InteropAUTClient {
		return "native_aut_web_interop_v1"
	}
	return "native_web_interop_v1"
}

func EndpointClient(task Task, c *NativeCase, role string) string {
	if !IsInteropClient(task.ClientType) {
		return task.ClientType
	}
	if role == "sender" {
		return c.SenderClient
	}
	return c.ReceiverClient
}
