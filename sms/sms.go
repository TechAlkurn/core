package sms

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/TechAlkurn/core/config"

	"github.com/twilio/twilio-go"
	twilioApi "github.com/twilio/twilio-go/rest/api/v2010"
)

/*
https://github.com/twilio/twilio-go
*/

func TwilioSendSms(to string, message string) error {
	sid := config.GetString("twilio_account_sid")
	token := config.GetString("twilio_auth_token")
	from := config.GetString("twilio_phone_number")
	client := twilio.NewRestClientWithParams(twilio.ClientParams{Username: sid, Password: token})
	// log.Println("accountSid: ", accountSid)
	// log.Println("authToken: ", authToken)
	// log.Println("fromNumber: ", fromNumber)
	params := &twilioApi.CreateMessageParams{}
	params.SetTo(to)
	params.SetFrom(from)
	params.SetBody(message)
	resp, err := client.Api.CreateMessage(params)
	if err != nil {
		fmt.Println("Error sending SMS message: " + err.Error())
	} else {
		_, err = json.Marshal(*resp)
		// fmt.Println("Response: " + string(response))
	}
	return err
}

/*
https://docs.unifonic.com/docs/sms-getting-started
*/
func UnifonicSendSms(to string, message string) error {
	sid := config.GetString("unifonic_account_sid")
	sender_id := config.GetString("unifonic_sender_id")

	client := &http.Client{}
	str := fmt.Sprintf("AppSid=%s&SenderID=%s&Body=%s&Recipient=%s&responseType=JSON&CorrelationID=CorrelationID&baseEncode=true&statusCallback=sent&async=false", sid, sender_id, message, to)
	data := strings.NewReader(str)
	req, err := http.NewRequest("POST", "https://el.cloud.unifonic.com/rest/SMS/messages", data)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println(err)
		return err
	}
	log.Printf("%s\n", bodyText)
	return nil
}
