package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

type Alert struct {
	Id                       string  `json:"_check_id"`
	Name                     string  `json:"_check_name"`
	Level                    string  `json:"_level"`
	Measurement              string  `json:"_measurement"`
	Message                  string  `json:"_message"`
	NotificationEndpointId   string  `json:"_notification_endpoint_id"`
	NotificationEndpointName string  `json:"_notification_endpoint_name"`
	NotificationRuleId       string  `json:"_notification_rule_id"`
	NotificationRuleName     string  `json:"_notification_rule_name"`
	SourceMeasurement        string  `json:"_source_measurement"`
	SourceTimestamp          int     `json:"_source_timestamp"`
	Start                    string  `json:"_start"`
	Stop                     string  `json:"_stop"`
	StatusTimestamp          int     `json:"_status_timestamp"`
	Time                     string  `json:"_time"`
	Type                     string  `json:"_type"`
	Value                    float64 `json:"_value"`
	Version                  int     `json:"_version"`
	DeviceId                 string  `json:"deviceID"`
	QueryField               string  `json:"query_field"`
}

func notify(w http.ResponseWriter, r *http.Request) {
	var alert Alert
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("reading body: %v", err)
	}
	if err = json.Unmarshal(body, &alert); err != nil {
		log.Printf("unmarshalling: %v", err)
	}
	log.Printf("received: %v", alert)
}

func main() {
	http.Handle("/notify", http.HandlerFunc(notify))
	if err := http.ListenAndServe("localhost:3030", nil); err != nil {
		log.Printf("while shutting down server: %v", err)
	}
}
