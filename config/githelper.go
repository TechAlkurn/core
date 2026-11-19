package components

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"app/pkg/config"

	"github.com/TechAlkurn/core/lib"
)

// Sends panic details to GitLab issues
func SendErrorAlert(recovered any, stack string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("sendErrorAlert panicked: %v\n", r)
		}
	}()
	_ = _send(fmt.Sprintf("*Panic recovered:*\n```\n%v\n\n%s\n```", recovered, stack))
}

func _send(description string) error {
	token := config.GetString("gitlab_token")
	projectID := config.GetString("gitlab_project_id")
	if lib.IsEmpty(projectID) {
		projectID = config.GetString("gitlab_project_id")
	}
	if token == "" || projectID == "" {
		return fmt.Errorf("missing GITLAB_TOKEN or GITLAB_PROJECT_ID")
	}
	apiURL := fmt.Sprintf("https://gitlab.com/api/v4/projects/%s/issues", projectID)
	form := url.Values{}
	form.Set("title", "Panic recovered by developer")
	form.Set("description", description)
	form.Set("labels", "bug")
	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("PRIVATE-TOKEN", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var body bytes.Buffer
		_, _ = body.ReadFrom(resp.Body)
		return fmt.Errorf("GitLab API error: %s - %s", resp.Status, body.String())
	}
	config.Log.Info("GitLab issue created successfully")
	return nil
}
