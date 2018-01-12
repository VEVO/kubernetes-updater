package main

import (
	"time"

	"gopkg.in/zorkian/go-datadog-api.v2"
)

type ddClientConfig struct {
	client *datadog.Client
}

func newDataDogClient(apiKey string, appKey string) *ddClientConfig {
	c := datadog.NewClient(apiKey, appKey)
	return &ddClientConfig{client: c}
}

func (c ddClientConfig) startDownTime(scope []string) (int, error) {
	start := time.Now().Unix()
	// Use default of 3 hours for downtime
	end := start + 10800

	downtime, err := c.client.CreateDowntime(&datadog.Downtime{
		Message: datadog.String("Downtime for kubernetes cluster roll"),
		Scope:   scope,
		End:     datadog.Int(int(end)),
	})

	return downtime.GetId(), err
}

func (c ddClientConfig) endDownTime(id int) error {
	err := c.client.DeleteDowntime(id)
	return err
}
