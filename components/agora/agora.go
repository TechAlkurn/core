package agora

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/TechAlkurn/core/config"

	rtctokenbuilder "github.com/AgoraIO-Community/go-tokenbuilder/rtctokenbuilder2"
	"github.com/TechAlkurn/core/lib"
)

/*
When a session is saved, a unique agora_meeting_id (channel name like session_a3b2c1d4) is stored. Teachers/students call
/user/index?key=agora-meeting-auth-endpoint&channelName=session_a3b2c1d4&uid=123&role=1
with {channelName, uid, role} to get their RTC token — no pre-registration needed.
*/
const (
	AGORA_API_URL = "https://api.agora.io"

	// RTC Token roles — values match Agora's RolePublisher/RoleSubscriber.
	AgoraRolePublisher  = 1
	AgoraRoleSubscriber = 2
)

type agoraService struct {
	appID          string
	appCertificate string
	customerID     string
	customerSecret string
}

// NewAgoraService initialises the Agora service from config.
// Required env keys:
//
//	agora_app_id, agora_app_certificate   – for token generation
//	agora_customer_id, agora_customer_secret – for REST API (Basic Auth)
func NewAgoraService() *agoraService {
	return &agoraService{
		appID:          config.GetString("agora_app_id"),
		appCertificate: config.GetString("agora_app_certificate"),
		customerID:     config.GetString("agora_customer_id"),
		customerSecret: config.GetString("agora_customer_secret"),
	}
}

// GenerateRtcToken builds an Agora AccessToken2 ("007") for RTC access using
// the official-community token builder. Hand-rolling AccessToken2 is fragile
// (two-stage HMAC key derivation, exact field ordering) — the library handles
// all of that.
//
//   - channelName: channel the user will join
//   - uid:         user UID as a string. Numeric uids ("0" or any unsigned int)
//     are passed through BuildTokenWithUid; non-numeric strings
//     go through BuildTokenWithUserAccount. The client SDK must
//     use the matching join API (joinChannel vs joinChannelByUserAccount).
//   - role:        AgoraRolePublisher or AgoraRoleSubscriber
//   - tokenExpireSec / privilegeExpireSec: lifetimes in seconds (relative).

/*
claude query: Can you dobble check if func (s *agoraService) GenerateRtcToken(channelName, uid string, role int, tokenExpireSec, privilegeExpireSec uint32) (string, error) { is any issue or generating wrong token because when we generate token from live site is working fine but when I used code for generating token it now working
*/
func (s *agoraService) GenerateRtcToken(channelName, uid string, role int, tokenExpireSec, privilegeExpireSec uint32) (string, error) {
	if s.appID == "" || s.appCertificate == "" {
		return "", fmt.Errorf("agora: appID or appCertificate not configured")
	}
	var r rtctokenbuilder.Role = rtctokenbuilder.RoleSubscriber
	if role == AgoraRolePublisher {
		r = rtctokenbuilder.RolePublisher
	}
	if uid == "" {
		uid = "0"
	}
	if n, err := strconv.ParseUint(uid, 10, 32); err == nil {
		return rtctokenbuilder.BuildTokenWithUid(s.appID, s.appCertificate, channelName, uint32(n), r, tokenExpireSec, privilegeExpireSec)
	}
	return rtctokenbuilder.BuildTokenWithUserAccount(s.appID, s.appCertificate, channelName, uid, r, tokenExpireSec, privilegeExpireSec)
}

// ─── REST API (Basic Auth: Customer ID / Customer Secret) ─────────────────────

func (s *agoraService) _clientDo(method, endpoint string, request []byte) (map[string]any, error) {
	requestURL := fmt.Sprintf("%s/%s", AGORA_API_URL, endpoint)
	var bodyReader io.Reader
	if len(request) > 0 {
		bodyReader = bytes.NewReader(request)
	}
	req, err := http.NewRequest(method, requestURL, bodyReader)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(s.customerID, s.customerSecret)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("agora API error %d: %s", resp.StatusCode, string(body))
	}

	var res map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &res); err != nil {
			config.Log.Printf("Agora response parse error: %s\n", string(body))
		}
	}
	return res, nil
}

// GetChannelInfo returns details for an active channel.
// Docs: GET /dev/v1/channel/{appId}/{channelName}
func (s *agoraService) GetChannelInfo(channelName string) (map[string]any, error) {
	return s._clientDo("GET", fmt.Sprintf("dev/v1/channel/%s/%s", s.appID, channelName), nil)
}

// GetChannelUsers returns the online users in a channel.
// Docs: GET /dev/v1/channel/user/{appId}/{channelName}
func (s *agoraService) GetChannelUsers(channelName string) (map[string]any, error) {
	return s._clientDo("GET", fmt.Sprintf("dev/v1/channel/user/%s/%s", s.appID, channelName), nil)
}

// BanChannelMember bans a user from a channel for the specified duration.
// Docs: POST /dev/v1/ban/member
func (s *agoraService) BanChannelMember(channelName string, uid uint32, durationSeconds int) (map[string]any, error) {
	body, _ := json.Marshal(map[string]any{
		"appid":           s.appID,
		"uid":             uid,
		"cname":           channelName,
		"time_in_seconds": durationSeconds,
	})
	return s._clientDo("POST", "dev/v1/ban/member", body)
}

// ─── Cloud Recording ──────────────────────────────────────────────────────────

// AcquireRecording acquires a cloud recording resource ID.
// Docs: POST /v1/apps/{appId}/cloud_recording/acquire
func (s *agoraService) AcquireRecording(channelName string, uid uint32) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"cname":         channelName,
		"uid":           fmt.Sprintf("%d", uid),
		"clientRequest": map[string]any{},
	})
	res, err := s._clientDo("POST", fmt.Sprintf("v1/apps/%s/cloud_recording/acquire", s.appID), body)
	if err != nil {
		return "", err
	}
	resourceId, _ := res["resourceId"].(string)
	if resourceId == "" {
		return "", fmt.Errorf("agora: empty resourceId in acquire response")
	}
	return resourceId, nil
}

// StartRecording starts a cloud recording session in mixed mode.
// storageConfig keys: vendor (int), region (int), bucket, accessKey, secretKey.
// Docs: POST /v1/apps/{appId}/cloud_recording/resourceid/{resourceId}/mode/mix/start
func (s *agoraService) StartRecording(channelName string, uid uint32, token, resourceId string, storageConfig map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(map[string]any{
		"cname": channelName,
		"uid":   fmt.Sprintf("%d", uid),
		"clientRequest": map[string]any{
			"token": token,
			"recordingConfig": map[string]any{
				"maxIdleTime":       30,
				"streamTypes":       2, // 0=audio 1=video 2=both
				"channelType":       0, // 0=communication 1=live
				"videoStreamType":   0,
				"subscribeUidGroup": 0,
			},
			"storageConfig": storageConfig,
		},
	})
	return s._clientDo(
		"POST",
		fmt.Sprintf("v1/apps/%s/cloud_recording/resourceid/%s/mode/mix/start", s.appID, resourceId),
		body,
	)
}

// StopRecording stops an ongoing cloud recording session.
// Docs: POST /v1/apps/{appId}/cloud_recording/resourceid/{resourceId}/sid/{sid}/mode/mix/stop
func (s *agoraService) StopRecording(channelName string, uid uint32, resourceId, sid string) (map[string]any, error) {
	body, _ := json.Marshal(map[string]any{
		"cname": channelName,
		"uid":   fmt.Sprintf("%d", uid),
		"clientRequest": map[string]any{
			"async_stop": false,
		},
	})
	return s._clientDo(
		"POST",
		fmt.Sprintf("v1/apps/%s/cloud_recording/resourceid/%s/sid/%s/mode/mix/stop", s.appID, resourceId, sid),
		body,
	)
}

// QueryRecording returns the status of an ongoing cloud recording session.
// Docs: GET /v1/apps/{appId}/cloud_recording/resourceid/{resourceId}/sid/{sid}/mode/mix/query
func (s *agoraService) QueryRecording(resourceId, sid string) (map[string]any, error) {
	return s._clientDo(
		"GET",
		fmt.Sprintf("v1/apps/%s/cloud_recording/resourceid/%s/sid/%s/mode/mix/query", s.appID, resourceId, sid),
		nil,
	)
}

// ─── SDK Auth Endpoint ────────────────────────────────────────────────────────

// MeetingSdkAuthEndpoint generates an Agora RTC token for a channel session.
// Expected body keys:
//
//	channelName (string, required) – the Agora channel to join
//	uid         (string|int)       – user's UID; "0" for dynamic (default "0")
//	role        (int)              – 1=publisher, 2=subscriber (default publisher)
//	expirationSeconds (int)        – token lifetime in seconds (default 3600)
func (s *agoraService) MeetingSdkAuthEndpoint(body map[string]any) (map[string]any, error) {
	channelName := lib.ToString(body["channelName"])
	if channelName == "" {
		return nil, fmt.Errorf("channelName is required")
	}

	uid := lib.ToString(body["uid"])
	if uid == "" {
		uid = "0"
	}
	role := AgoraRolePublisher
	if r, ok := body["role"]; ok && lib.ToInt32(r) == int32(AgoraRoleSubscriber) {
		role = AgoraRoleSubscriber
	}
	tokenExpire := uint32(3600)
	if exp, ok := body["expirationSeconds"]; ok {
		if v := lib.ToUint32(exp); v > 0 {
			tokenExpire = v
		}
	}
	config.Log.Printf("Agora token request: channel=%s uid=%s role=%d expire=%d\n", channelName, uid, role, tokenExpire)
	token, err := s.GenerateRtcToken(channelName, uid, role, tokenExpire, tokenExpire)
	if err != nil {
		return nil, err
	}
	// Always return uid as a number when it is numeric, so the client can
	// pass it straight to client.join() without the string-vs-number trap
	// (numeric uid uses joinChannel, string uid uses joinChannelByUserAccount —
	// mixing them on the same channel silently breaks audio/video subscription).
	var uidOut any = uid
	if n, err := strconv.ParseUint(uid, 10, 32); err == nil {
		uidOut = uint32(n)
	}
	return map[string]any{
		"user_token": token,
		//	"appId":       s.appID,
		"channel": channelName,
		"uid":     uidOut,
		"expire":  tokenExpire,
	}, nil
}
