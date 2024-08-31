package curie

import "time"

type InspectResponse struct {
	Info struct {
		Config struct {
			SharedDirectory struct {
				Directories []any `json:"directories"`
			} `json:"sharedDirectory"`
			Shutdown struct {
				Behaviour struct {
					Stop struct {
					} `json:"stop"`
				} `json:"behaviour"`
			} `json:"shutdown"`
			Network struct {
				Devices []struct {
					MacAddress string `json:"macAddress"`
					Mode       string `json:"mode"`
				} `json:"devices"`
			} `json:"network"`
			MemorySize struct {
				Bytes int64 `json:"bytes"`
			} `json:"memorySize"`
			CPUCount int `json:"cpuCount"`
			Display  struct {
				Height        int `json:"height"`
				PixelsPerInch int `json:"pixelsPerInch"`
				Width         int `json:"width"`
			} `json:"display"`
		} `json:"config"`
		Metadata struct {
			CreatedAt time.Time `json:"createdAt"`
			ID        string    `json:"id"`
			Network   struct {
				Devices struct {
					Num0 struct {
						MACAddress string `json:"MACAddress"`
					} `json:"0"`
				} `json:"devices"`
			} `json:"network"`
		} `json:"metadata"`
	} `json:"info"`
	Arp []struct {
		IP         string `json:"ip"`
		MacAddress string `json:"macAddress"`
	} `json:"arp"`
}
