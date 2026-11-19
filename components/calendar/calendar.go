package calendar

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/TechAlkurn/core/config"
	"github.com/TechAlkurn/core/db/activerecord"

	"github.com/TechAlkurn/core/lib"
	"golang.org/x/oauth2"
	calendar "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

type EventDetails struct {
	Summary     string                    `json:"summary"`
	Location    string                    `json:"location"`
	Description string                    `json:"description"`
	StartTime   string                    `json:"startTime"`
	EndTime     string                    `json:"endTime"`
	TimeZone    string                    `json:"timeZone"`
	EventType   string                    `json:"eventType"`
	Attendees   []*calendar.EventAttendee `json:"attendees"`
}

type EventCalendar interface {
	EventList(pageToken string) (items []*calendar.Event, err error)
	EventInsert(ed *EventDetails) (event *calendar.Event, err error)
	EventUpdate(ed *EventDetails, eventId string) (event *calendar.Event, err error)
	EventView(eventId string) (*calendar.Event, error)
	EventDelete(eventId string) error
}

type eventCalendar struct {
	Client     *http.Client
	CalendarId string
}

func RefreshToken(loginId uint32) (data map[string]any, err error) {
	var profile map[string]any
	db := activerecord.Raw("SELECT * from `user_profile` WHERE `user_id` = ?", loginId).Scan(&profile)
	if db.Error != nil || profile == nil {
		return nil, errors.New("user profile not found")
	}
	googleRefreshToken := profile["google_refresh_token"]
	if lib.Empty(googleRefreshToken) {
		return nil, errors.New("refresh token not found")
	}
	clientId := config.GetString("google_client_id")
	clientSecret := config.GetString("google_client_secret")
	url := fmt.Sprintf("https://oauth2.googleapis.com/token?client_id=%s&client_secret=%s&grant_type=refresh_token&refresh_token=%s", clientId, clientSecret, googleRefreshToken)
	// config.Log.Printf("url: %v\n", url)
	// os.Exit(1)
	client := &http.Client{}
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Connection", "keep-alive")
	res, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	data = lib.ToMapped(body)
	return data, nil
}

func NewCalendarServiceV2(accessToken string) EventCalendar {
	token := &oauth2.Token{AccessToken: accessToken}
	// Create an HTTP client with the token
	client := (&oauth2.Config{}).Client(context.Background(), token)

	return &eventCalendar{
		Client:     client,
		CalendarId: "primary",
	}
}

func (e *eventCalendar) EventList(pageToken string) (items []*calendar.Event, err error) {
	// Create a new Calendar service using option.WithHTTPClient
	srv, err := calendar.NewService(context.Background(), option.WithHTTPClient(e.Client))
	if err != nil {
		return nil, fmt.Errorf("unable to create Calendar service: %v", err)
	}

	// Fetch Calendar Events
	t := time.Now().Format(time.RFC3339)
	events, err := srv.Events.List(e.CalendarId).
		ShowDeleted(false).
		SingleEvents(true).
		TimeMin(t).
		MaxResults(500).
		PageToken(pageToken).
		OrderBy("startTime").
		Do()
	if err != nil {
		// log.Fatalf("Unable to retrieve calendar events: %v", err)
		return nil, err
	}
	// Display Events
	if len(events.Items) == 0 {
		return nil, errors.New("no upcoming events found")
	}
	return events.Items, nil
}

func (e *eventCalendar) EventInsert(ed *EventDetails) (event *calendar.Event, err error) {
	// Define the new event

	event = &calendar.Event{
		Summary:     ed.Summary,
		Location:    ed.Location,
		Description: ed.Description,
		Start: &calendar.EventDateTime{
			DateTime: ed.StartTime, // Use ISO 8601 format
			TimeZone: ed.TimeZone,
		},
		End: &calendar.EventDateTime{
			DateTime: ed.EndTime,
			TimeZone: ed.TimeZone,
		},
		Attendees: ed.Attendees,
		Reminders: &calendar.EventReminders{
			UseDefault: true,
		},
	}

	// config.Log.Printf("event.Start.DateTime %v\n", event.Start.DateTime)
	// config.Log.Printf("event.End.DateTime: %v\n", event.End.DateTime)

	// Create a new Calendar service using option.WithHTTPClient
	srv, err := calendar.NewService(context.Background(), option.WithHTTPClient(e.Client))
	if err != nil {
		return nil, fmt.Errorf("unable to create Calendar service: %v", err)
	}
	event, err = srv.Events.Insert(e.CalendarId, event).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to create calendar service: %v", err)
	}
	//	fmt.Printf("Event created: %s\n", event.HtmlLink)
	return
}

func (e *eventCalendar) EventUpdate(ed *EventDetails, eventId string) (event *calendar.Event, err error) {
	// Create a new Calendar service using option.WithHTTPClient
	srv, err := calendar.NewService(context.Background(), option.WithHTTPClient(e.Client))
	if err != nil {
		return nil, fmt.Errorf("unable to create calendar service: %v", err)
	}

	// Fetch the existing event
	event, err = srv.Events.Get(e.CalendarId, eventId).Do()
	if err != nil {
		log.Fatalf("Unable to retrieve event: %v", err)
		return nil, err
	}
	// Modify event details
	event.Summary = ed.Summary
	event.Description = ed.Description
	event.Location = ed.Location
	event.Start = &calendar.EventDateTime{
		DateTime: ed.StartTime,
		TimeZone: ed.TimeZone,
	}
	event.End = &calendar.EventDateTime{
		DateTime: ed.EndTime,
		TimeZone: ed.TimeZone,
	}
	// Update the event
	event, err = srv.Events.Update(e.CalendarId, event.Id, event).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to create calendar service: %v", err)
	}

	return
}

func (e *eventCalendar) EventDelete(eventId string) error {
	// Create a new Calendar service using option.WithHTTPClient
	srv, err := calendar.NewService(context.Background(), option.WithHTTPClient(e.Client))
	if err != nil {
		return fmt.Errorf("unable to create calendar service: %v", err)
	}
	// Delete the event
	if err := srv.Events.Delete(e.CalendarId, eventId).Do(); err != nil {
		return err
	}
	fmt.Println("Event deleted successfully")
	return nil
}

func (e *eventCalendar) EventView(eventId string) (*calendar.Event, error) {
	// Create a new Calendar service using option.WithHTTPClient
	srv, err := calendar.NewService(context.Background(), option.WithHTTPClient(e.Client))
	if err != nil {
		return nil, fmt.Errorf("unable to create calendar service: %v", err)
	}
	// Delete the event
	et, err := srv.Events.Get(e.CalendarId, eventId).Do()
	if err != nil {
		return nil, fmt.Errorf("event not found: %v", err)
	}
	return et, nil
}
