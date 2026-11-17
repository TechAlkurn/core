package components

import (
	"context"
	"fmt"

	"github.com/TechAlkurn/core/lib"

	firebase "firebase.google.com/go"
	"firebase.google.com/go/messaging"
)

type (
	FcmService interface {
		PushNotifications() (err error)
	}
	fcmService struct {
		Config *ConfigFcm
	}
)

func NewFcmService(config *ConfigFcm) FcmService {
	srv := &fcmService{
		Config: config,
	}
	return srv
}

func (srv *fcmService) initializeFirebaseApp() (app *firebase.App, err error) {
	if app, err = firebase.NewApp(context.Background(), nil); err != nil {
		return
	}
	return
}

func (srv *fcmService) initializeFirebaseClient() (client *messaging.Client, err error) {
	app, err := srv.initializeFirebaseApp()
	if err != nil {
		return
	}
	client, err = app.Messaging(context.Background())
	if err != nil {
		return
	}
	return
}

func (srv *fcmService) PushNotifications() (err error) {
	if !lib.IsEmpty(srv.Config.Token) {
		if _, err = srv.SendSpecificDevice(); err != nil {
			return
		}
	}
	if len(srv.Config.Tokens) > 1 {
		// srv.SendMultipleDevices()
		if _, _, err = srv.SendMultipleDevicesIndividually(); err != nil {
			return
		}
	}
	if !lib.IsEmpty(srv.Config.Topic) {
		if _, err = srv.SendTopic(); err != nil {
			return
		}
	}
	return
}

func (srv *fcmService) SendSpecificDevice() (response string, err error) {
	client, err := srv.initializeFirebaseClient()
	if err != nil {
		return
	}
	response, err = client.Send(context.Background(), &messaging.Message{
		Notification: &messaging.Notification{
			Title: srv.Config.Title,
			Body:  srv.Config.Body,
		},
		Data: map[string]string{
			"title": srv.Config.Title,
			"body":  srv.Config.Body,
		},
		Token: lib.CurrentString(srv.Config.Tokens, 0), // it's a single device token
	})
	if err != nil {
		return
	}
	return
}

func (srv *fcmService) SendMultipleDevicesIndividually() (responses []string, failedTokens []string, err error) {
	client, err := srv.initializeFirebaseClient()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize Firebase client: %v", err)
	}

	// Loop through each token and send individually
	for _, token := range srv.Config.Tokens {
		response, err := client.Send(context.Background(), &messaging.Message{
			Notification: &messaging.Notification{
				Title: srv.Config.Title,
				Body:  srv.Config.Body,
			},
			Data: map[string]string{
				"title": srv.Config.Title,
				"body":  srv.Config.Body,
			},
			Token: token, // it's a single device token
		})

		if err != nil {
			// Log the specific error for each failed token
			fmt.Printf("Error sending to token %s: %v\n", token, err)
			failedTokens = append(failedTokens, token)
		} else {
			// Collect successful responses
			responses = append(responses, response)
		}
	}

	// Return an error if there were any failed tokens
	if len(failedTokens) > 0 {
		err = fmt.Errorf("some tokens failed: %v", failedTokens)
		fmt.Printf("SendMultipleDevicesIndividually error: %v\n", err)
	}

	return responses, failedTokens, err
}

func (srv *fcmService) SendMultipleDevices() (response *messaging.BatchResponse, err error) {
	client, err := srv.initializeFirebaseClient()
	if err != nil {
		return
	}
	response, err = client.SendMulticast(context.Background(), &messaging.MulticastMessage{
		Notification: &messaging.Notification{
			Title: srv.Config.Title,
			Body:  srv.Config.Body,
		},
		Data: map[string]string{
			"title": srv.Config.Title,
			"body":  srv.Config.Body,
		},
		Tokens: srv.Config.Tokens, // it's an array of device tokens
	})
	if err != nil {
		return
	}

	if response.FailureCount > 0 {
		var failedTokens []string
		for idx, resp := range response.Responses {
			if !resp.Success {
				// The order of responses corresponds to the order of the registration tokens.
				failedTokens = append(failedTokens, srv.Config.Tokens[idx])
			}
		}
		err = fmt.Errorf("list of tokens that caused failures: %v", failedTokens)
		fmt.Printf("multiple response: %v", err)
	}

	return
}

func (srv *fcmService) SendTopic() (response string, err error) {
	client, err := srv.initializeFirebaseClient()
	if err != nil {
		return
	}

	message := &messaging.Message{
		Notification: &messaging.Notification{
			Title: srv.Config.Title,
			Body:  srv.Config.Body,
		},
		Data: map[string]string{
			"title": srv.Config.Title,
			"body":  srv.Config.Body,
		},
		Topic: srv.Config.Topic,
	}

	response, err = client.Send(context.Background(), message)
	if err != nil {
		return
	}
	return
}
