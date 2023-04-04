package fcm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"strings"
)

const (
	defaultEndpoint      = "https://fcm.googleapis.com/v1"
	defaultBatchEndpoint = "https://fcm.googleapis.com/batch"

	apiFormatVersionHeader = "X-GOOG-API-FORMAT-VERSION"
	apiFormatVersion       = "2"
)

// Client abstracts the interaction between the application server and the
// FCM server via HTTP protocol. The developer must obtain a service account
// private key in JSON and the Firebase project id from the Firebase console and pass it to the `Client`
// so that it can perform authorized requests on the application server's behalf.
type Client struct {
	// https://firebase.google.com/docs/reference/fcm/rest/v1/projects.messages/send
	projectID     string
	fcmEndpoint   string
	batchEndpoint string

	// the endpoint for the project
	sendEndpoint  string

	client        *http.Client
	tokenProvider *tokenProvider
}

// NewClient creates new Firebase Cloud Messaging Client based on a json service account file credentials file.
func NewClient(projectID string, credentialsLocation string, opts ...Option) (*Client, error) {
	tp, err := newTokenProvider(credentialsLocation)
	if err != nil {
		return nil, err
	}

	c := &Client{
		projectID:     projectID,
		fcmEndpoint:   defaultEndpoint,
		batchEndpoint: defaultBatchEndpoint,
		client:        http.DefaultClient,
		tokenProvider: tp,
	}
	c.sendEndpoint = fmt.Sprintf("%s/projects/%s/messages:send", c.fcmEndpoint, c.projectID)

	for _, o := range opts {
		if err := o(c); err != nil {
			return nil, err
		}
	}

	return c, nil
}

// Send sends a message to the FCM server.
func (c *Client) Send(ctx context.Context, req *SendRequest) (string, error) {
	// validate
	if err := req.Message.Validate(); err != nil {
		return "", err
	}

	// marshal message
	data, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	return c.send(ctx, data)
}

// send sends a request.
func (c *Client) send(ctx context.Context, data []byte) (messageID string, err error) {
	// create request
	req, err := http.NewRequestWithContext(ctx, "POST", c.sendEndpoint, bytes.NewBuffer(data))
	if err != nil {
		return "", err
	}

	// get bearer token
	token, err := c.tokenProvider.token()
	if err != nil {
		return "", err
	}

	// add headers
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Add("Content-Type", "application/json")

	// execute request
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		requestBytes, _ := httputil.DumpRequest(req, true)
		responseBytes, _ := httputil.DumpResponse(resp, true)

		if resp.StatusCode >= http.StatusInternalServerError {
			return "", HttpError{
				RequestDump:  string(requestBytes),
				ResponseDump: string(responseBytes),
				Err:          fmt.Errorf(fmt.Sprintf("%d error: %s", resp.StatusCode, resp.Status)),
			}
		}
		return "", HttpError{
			RequestDump:  string(requestBytes),
			ResponseDump: string(responseBytes),
			Err:          fmt.Errorf("%d error: %s", resp.StatusCode, resp.Status),
		}
	}

	type MessageResponse struct {
		// The identifier of the message sent, in the format of projects/*/messages/{message_id}.
		Name string `json:"name,omitempty"`
	}

	response := new(MessageResponse)
	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return "", err
	}

	lastIndex := strings.LastIndex(response.Name, "/")
	if len(response.Name) > lastIndex {
		return response.Name[lastIndex+1:], nil
	}

	return "", nil
}

// HttpError contains the dump of the request and response for debugging purposes.
type HttpError struct {
	RequestDump  string
	ResponseDump string
	Err          error
}

func (fcmError HttpError) Error() string {
	return fcmError.Err.Error()
}
