package fcm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httputil"
	"net/textproto"
)

const (
	maxMessages       = 500
	multipartBoundary = "__END_OF_PART__"
)

// MulticastMessage represents a message that can be sent to multiple devices via Firebase Cloud
// Messaging (FCM).
//
// It contains payload information as well as the list of device registration tokens to which the
// message should be sent. A single MulticastMessage may contain up to 500 registration tokens.
type MulticastMessage struct {
	Tokens  []string
	Message *Message
}

// BatchResponse represents the response from the `SendAll()` and `SendMulticast()` APIs.
type MulticastResponse struct {
	SuccessCount int
	FailureCount int
	Responses    []*SendResponse
}

func (mm *MulticastMessage) toMessages() ([]*Message, error) {
	if len(mm.Tokens) == 0 {
		return nil, errors.New("tokens must not be nil or empty")
	}
	if len(mm.Tokens) > maxMessages {
		return nil, fmt.Errorf("tokens must not contain more than %d elements", maxMessages)
	}

	var messages []*Message
	for _, token := range mm.Tokens {
		temp := &Message{
			Token:        token,
			Data:         mm.Message.Data,
			Notification: mm.Message.Notification,
			Android:      mm.Message.Android,
			Webpush:      mm.Message.Webpush,
			Apns:         mm.Message.Apns,
		}
		messages = append(messages, temp)
	}

	return messages, nil
}

// SendAll sends the messages in the given array via Firebase Cloud Messaging.
//
// The messages array may contain up to 500 messages. SendAll employs batching to send the entire
// array of mssages as a single RPC call. Compared to the `Send()` function,
// this is a significantly more efficient way to send multiple messages. The responses list
// obtained from the return value corresponds to the order of the input messages. An error from
// SendAll indicates a total failure -- i.e. none of the messages in the array could be sent.
// Partial failures are indicated by a `BatchResponse` return value.
func (c *Client) SendAll(ctx context.Context, messages []*Message) (*MulticastResponse, error) {
	return c.sendBatch(ctx, messages, false)
}

// SendAllDryRun sends the messages in the given array via Firebase Cloud Messaging in the
// dry run (validation only) mode.
//
// This function does not actually deliver any messages to target devices. Instead, it performs all
// the SDK-level and backend validations on the messages, and emulates the send operation.
//
// The messages array may contain up to 500 messages. SendAllDryRun employs batching to send the
// entire array of mssages as a single RPC call. Compared to the `SendDryRun()` function, this
// is a significantly more efficient way to validate sending multiple messages. The responses list
// obtained from the return value corresponds to the order of the input messages. An error from
// SendAllDryRun indicates a total failure -- i.e. none of the messages in the array could be sent
// for validation. Partial failures are indicated by a `BatchResponse` return value.
func (c *Client) SendAllDryRun(ctx context.Context, messages []*Message) (*MulticastResponse, error) {
	return c.sendBatch(ctx, messages, true)
}

// SendMulticast sends the given multicast message to all the FCM registration tokens specified.
//
// The tokens array in MulticastMessage may contain up to 500 tokens. SendMulticast uses the
// `SendAll()` function to send the given message to all the target recipients. The
// responses list obtained from the return value corresponds to the order of the input tokens. An
// error from SendMulticast indicates a total failure -- i.e. the message could not be sent to any
// of the recipients. Partial failures are indicated by a `BatchResponse` return value.
func (c *Client) SendMulticast(ctx context.Context, message *MulticastMessage) (*MulticastResponse, error) {
	messages, err := toMessages(message)
	if err != nil {
		return nil, err
	}

	return c.SendAll(ctx, messages)
}

// SendMulticastDryRun sends the given multicast message to all the specified FCM registration
// tokens in the dry run (validation only) mode.
//
// This function does not actually deliver any messages to target devices. Instead, it performs all
// the SDK-level and backend validations on the messages, and emulates the send operation.
//
// The tokens array in MulticastMessage may contain up to 500 tokens. SendMulticastDryRun uses the
// `SendAllDryRun()` function to send the given message. The responses list obtained from
// the return value corresponds to the order of the input tokens. An error from SendMulticastDryRun
// indicates a total failure -- i.e. none of the messages were sent to FCM for validation. Partial
// failures are indicated by a `BatchResponse` return value.
func (c *Client) SendMulticastDryRun(ctx context.Context, message *MulticastMessage) (*MulticastResponse, error) {
	messages, err := toMessages(message)
	if err != nil {
		return nil, err
	}

	return c.SendAllDryRun(ctx, messages)
}

func toMessages(message *MulticastMessage) ([]*Message, error) {
	if message == nil {
		return nil, errors.New("message must not be nil")
	}

	return message.toMessages()
}

func (c *Client) sendBatch(ctx context.Context, messages []*Message, dryRun bool) (*MulticastResponse, error) {
	if len(messages) == 0 {
		return nil, errors.New("messages must not be nil or empty")
	}

	if len(messages) > maxMessages {
		return nil, fmt.Errorf("messages must not contain more than %d elements", maxMessages)
	}

	request, err := c.newBatchRequest(messages, dryRun)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		requestBytes, _ := httputil.DumpRequest(request, true)
		responseBytes, _ := httputil.DumpResponse(resp, true)

		if resp.StatusCode >= http.StatusInternalServerError {
			return nil, HttpError{
				RequestDump:  string(requestBytes),
				ResponseDump: string(responseBytes),
				Err:          fmt.Errorf(fmt.Sprintf("%d error: %s", resp.StatusCode, resp.Status)),
			}
		}
		return nil, HttpError{
			RequestDump:  string(requestBytes),
			ResponseDump: string(responseBytes),
			Err:          fmt.Errorf("%d error: %s", resp.StatusCode, resp.Status),
		}
	}

	return newBatchResponse(resp)
}

// part represents a HTTP request that can be sent embedded in a multipart batch request.
//
// See https://cloud.google.com/compute/docs/api/how-tos/batch for details on how GCP APIs support multipart batch
// requests.
type part struct {
	method  string
	url     string
	headers map[string]string
	body    interface{}
}

// multipartEntity represents an HTTP entity that consists of multiple HTTP requests (parts).
type multipartEntity struct {
	parts []*part
}

type fcmRequest struct {
	ValidateOnly bool     `json:"validate_only,omitempty"`
	Message      *Message `json:"message,omitempty"`
}

type fcmResponse struct {
	Name string `json:"name"`
}

type fcmErrorResponse struct {
	Error struct {
		Details []struct {
			Type      string `json:"@type"`
			ErrorCode string `json:"errorCode"`
		}
	} `json:"error"`
}

func (c *Client) newBatchRequest(messages []*Message, dryRun bool) (*http.Request, error) {
	headers := map[string]string{
		apiFormatVersionHeader: apiFormatVersion,
	}

	var parts []*part
	for idx, m := range messages {
		if err := m.Validate(); err != nil {
			return nil, fmt.Errorf("invalid message at index %d: %v", idx, err)
		}
		p := &part{
			method: http.MethodPost,
			url:    c.sendEndpoint,
			body: &fcmRequest{
				Message:      m,
				ValidateOnly: dryRun,
			},
			headers: headers,
		}
		parts = append(parts, p)
	}

	body := &multipartEntity{parts: parts}
	bodyBytes, err := body.Bytes()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.batchEndpoint, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}

	// get bearer token
	token, err := c.tokenProvider.token()
	if err != nil {
		return nil, err
	}

	// add headers
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Add("Content-Type", body.Mime())

	return req, nil
}

func newBatchResponse(resp *http.Response) (*MulticastResponse, error) {
	_, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("error parsing content-type header: %v", err)
	}

	mr := multipart.NewReader(resp.Body, params["boundary"])
	var responses []*SendResponse
	successCount := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		sr, err := newSendResponse(part)
		if err != nil {
			return nil, err
		}

		responses = append(responses, sr)
		if sr.Success {
			successCount++
		}
	}

	return &MulticastResponse{
		Responses:    responses,
		SuccessCount: successCount,
		FailureCount: len(responses) - successCount,
	}, nil
}

func newSendResponse(part *multipart.Part) (*SendResponse, error) {
	hr, err := http.ReadResponse(bufio.NewReader(part), nil)
	if err != nil {
		return nil, fmt.Errorf("error parsing multipart body: %v", err)
	}

	b, err := ioutil.ReadAll(hr.Body)
	if err != nil {
		return nil, err
	}

	if hr.StatusCode != http.StatusOK {
		return &SendResponse{
			Success:   false,
			ErrorCode: hr.StatusCode,
			ErrorBody: string(b),
		}, nil
	}

	var result fcmResponse
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, err
	}

	return &SendResponse{
		Success:   true,
		MessageID: result.Name,
	}, nil
}

func (e *multipartEntity) Mime() string {
	return fmt.Sprintf("multipart/mixed; boundary=%s", multipartBoundary)
}

func (e *multipartEntity) Bytes() ([]byte, error) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	writer.SetBoundary(multipartBoundary)
	for idx, part := range e.parts {
		if err := part.writeTo(writer, idx); err != nil {
			return nil, err
		}
	}

	writer.Close()
	return buffer.Bytes(), nil
}

func (p *part) writeTo(writer *multipart.Writer, idx int) error {
	b, err := p.bytes()
	if err != nil {
		return err
	}

	header := make(textproto.MIMEHeader)
	header.Add("Content-Length", fmt.Sprintf("%d", len(b)))
	header.Add("Content-Type", "application/http")
	header.Add("Content-Id", fmt.Sprintf("%d", idx+1))
	header.Add("Content-Transfer-Encoding", "binary")

	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}

	_, err = part.Write(b)
	return err
}

func (p *part) bytes() ([]byte, error) {
	b, err := json.Marshal(p.body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(p.method, p.url, bytes.NewBuffer(b))
	if err != nil {
		return nil, err
	}

	for key, value := range p.headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("User-Agent", "")

	var buffer bytes.Buffer
	if err := req.Write(&buffer); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}
