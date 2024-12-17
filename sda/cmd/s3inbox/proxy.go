package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/minio/minio-go/v6/pkg/signer"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"
	log "github.com/sirupsen/logrus"
)

// Proxy represents the toplevel object in this application
type Proxy struct {
	s3        storage.S3Conf
	auth      userauth.Authenticator
	messenger *broker.AMQPBroker
	database  *database.SDAdb
	client    *http.Client
	fileIds   map[string]string
}

// The Event struct
type Event struct {
	Operation string        `json:"operation"`
	Username  string        `json:"user"`
	Filepath  string        `json:"filepath"`
	Filesize  int64         `json:"filesize"`
	Checksum  []interface{} `json:"encrypted_checksums"`
}

// Checksum used in the message
type Checksum struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// S3RequestType is the type of request that we are currently proxying to the
// backend
type S3RequestType int

type ErrorResponse struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}

// The different types of requests
const (
	MakeBucket S3RequestType = iota
	RemoveBucket
	List
	Put
	Get
	Delete
	AbortMultipart
	Policy
	Other
)

// NewProxy creates a new S3Proxy. This implements the ServerHTTP interface.
func NewProxy(s3conf storage.S3Conf, auth userauth.Authenticator, messenger *broker.AMQPBroker, database *database.SDAdb, tls *tls.Config) *Proxy {
	tr := &http.Transport{TLSClientConfig: tls}
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second}

	return &Proxy{s3conf, auth, messenger, database, client, make(map[string]string)}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token, err := p.auth.Authenticate(r)
	if err != nil {
		log.Debugln("Request not authenticated")
		p.notAuthorized(w, r)

		return
	}

	switch t := p.detectRequestType(r); t {
	case MakeBucket, RemoveBucket, Delete, Policy, Get:
		// Not allowed
		log.Debug("not allowed known")
		p.notAllowedResponse(w, r)
	case Put, List, Other, AbortMultipart:
		// Allowed
		log.Debug("allowed known")
		p.allowedResponse(w, r, token)
	default:
		log.Debugf("Unexpected request (%v) not allowed", r)
		p.notAllowedResponse(w, r)
	}
}

// Report 500 to the user, log the original error
func (p *Proxy) internalServerError(w http.ResponseWriter, r *http.Request, err string) {
	log.Error(err)
	msg := fmt.Sprintf("Internal server error for request (%v)", r)
	reportError(http.StatusInternalServerError, msg, w)
}

func (p *Proxy) notAllowedResponse(w http.ResponseWriter, _ *http.Request) {
	reportError(http.StatusForbidden, "not allowed response", w)
}

func (p *Proxy) notAuthorized(w http.ResponseWriter, _ *http.Request) {
	reportError(http.StatusUnauthorized, "not authorized", w)
}

func (p *Proxy) allowedResponse(w http.ResponseWriter, r *http.Request, token jwt.Token) {
	log.Debug("prepend")
	// Check whether token username and filepath match
	str, err := url.ParseRequestURI(r.URL.Path)
	if err != nil || str.Path == "" {
		reportError(http.StatusBadRequest, err.Error(), w)
	}

	path := strings.Split(str.Path, "/")
	if strings.Contains(token.Subject(), "@") {
		if strings.ReplaceAll(token.Subject(), "@", "_") != path[1] {
			reportError(http.StatusBadRequest, fmt.Sprintf("token supplied username: %s, but URL had: %s", token.Subject(), path[1]), w)

			return
		}
	} else if token.Subject() != path[1] {
		reportError(http.StatusBadRequest, fmt.Sprintf("token supplied username: %s, but URL had: %s", token.Subject(), path[1]), w)

		return
	}
	err = p.prependBucketToHostPath(r)
	if err != nil {
		reportError(http.StatusBadRequest, err.Error(), w)
	}

	username := token.Subject()
	rawFilepath := strings.Replace(r.URL.Path, "/"+p.s3.Bucket+"/", "", 1)
	anonymizedFilepath := helper.AnonymizeFilepath(rawFilepath, username)

	filepath, err := formatUploadFilePath(anonymizedFilepath)
	if err != nil {
		reportError(http.StatusNotAcceptable, err.Error(), w)

		return
	}

	// register file in database if it's the start of an upload
	if p.detectRequestType(r) == Put && p.fileIds[r.URL.Path] == "" {
		log.Debugf("registering file %v in the database", r.URL.Path)
		p.fileIds[r.URL.Path], err = p.database.RegisterFile(filepath, username)
		log.Debugf("fileId: %v", p.fileIds[r.URL.Path])
		if err != nil {
			p.internalServerError(w, r, fmt.Sprintf("failed to register file in database: %v", err))

			return
		}
	}

	log.Debug("Forwarding to backend")
	s3response, err := p.forwardToBackend(r)
	if err != nil {
		p.internalServerError(w, r, fmt.Sprintf("forwarding error: %v", err))

		return
	}

	// Send message to upstream and set file as uploaded in the database
	if p.uploadFinishedSuccessfully(r, s3response) {
		log.Debug("create message")
		message, err := p.CreateMessageFromRequest(r, token, anonymizedFilepath)
		if err != nil {
			p.internalServerError(w, r, err.Error())

			return
		}
		jsonMessage, err := json.Marshal(message)
		if err != nil {
			p.internalServerError(w, r, fmt.Sprintf("failed to marshal rabbitmq message to json: %v", err))

			return
		}

		err = p.checkAndSendMessage(jsonMessage, r)
		if err != nil {
			p.internalServerError(w, r, fmt.Sprintf("broker error: %v", err))

			return
		}

		log.Debugf("marking file %v as 'uploaded' in database", p.fileIds[r.URL.Path])
		err = p.database.UpdateFileEventLog(p.fileIds[r.URL.Path], "uploaded", p.fileIds[r.URL.Path], "inbox", "{}", string(jsonMessage))
		if err != nil {
			p.internalServerError(w, r, fmt.Sprintf("could not connect to db: %v", err))

			return
		}

		delete(p.fileIds, r.URL.Path)
	}

	// Writing non-200 to the response before the headers propagate the error
	// to the s3cmd client.
	// Writing 200 here breaks uploads though, and writing non-200 codes after
	// the headers results in the error message always being
	// "MD5 Sums don't match!".
	if s3response.StatusCode < 200 || s3response.StatusCode > 299 {
		w.WriteHeader(s3response.StatusCode)
	}

	// Redirect answer
	log.Debug("redirect answer")
	for header, values := range s3response.Header {
		for _, value := range values {
			w.Header().Add(header, value)
		}
	}

	_, err = io.Copy(w, s3response.Body)
	if err != nil {
		p.internalServerError(w, r, fmt.Sprintf("redirect error: %v", err))

		return
	}

	// Read any remaining data in the connection and
	// Close so connection can be reused.
	_, _ = io.ReadAll(s3response.Body)
	_ = s3response.Body.Close()
}

// Renew the connection to MQ if necessary, then send message
func (p *Proxy) checkAndSendMessage(jsonMessage []byte, r *http.Request) error {
	var err error
	if p.messenger == nil {
		return fmt.Errorf("messenger is down")
	}
	if p.messenger.IsConnClosed() {
		log.Warning("connection is closed, reconnecting...")
		p.messenger, err = broker.NewMQ(p.messenger.Conf)
		if err != nil {
			return err
		}
	}

	if p.messenger.Channel.IsClosed() {
		log.Warning("channel is closed, recreating...")
		err := p.messenger.CreateNewChannel()
		if err != nil {
			return err
		}
	}

	log.Debugf("Sending message with id %s", p.fileIds[r.URL.Path])
	if err := p.messenger.SendMessage(p.fileIds[r.URL.Path], p.messenger.Conf.Exchange, p.messenger.Conf.RoutingKey, jsonMessage); err != nil {
		return fmt.Errorf("error when sending message to broker: %v", err)
	}

	return nil
}

func (p *Proxy) uploadFinishedSuccessfully(req *http.Request, response *http.Response) bool {
	if response.StatusCode != 200 {
		return false
	}

	switch req.Method {
	case http.MethodPut:
		if !strings.Contains(req.URL.String(), "partNumber") {
			return true
		}

		return false
	case http.MethodPost:
		if strings.Contains(req.URL.String(), "uploadId") {
			return true
		}

		return false
	default:

		return false
	}
}

func (p *Proxy) forwardToBackend(r *http.Request) (*http.Response, error) {
	p.resignHeader(r, p.s3.AccessKey, p.s3.SecretKey, fmt.Sprintf("%s:%d", p.s3.URL, p.s3.Port))

	// Redirect request
	nr, err := http.NewRequest(r.Method, fmt.Sprintf("%s:%d", p.s3.URL, p.s3.Port)+r.URL.String(), r.Body)
	if err != nil {
		log.Debug("error when redirecting the request")
		log.Debug(err)

		return nil, err
	}
	nr.Header = r.Header
	contentLength, _ := strconv.ParseInt(r.Header.Get("content-length"), 10, 64)
	nr.ContentLength = contentLength

	return p.client.Do(nr)
}

// Add bucket to host path
func (p *Proxy) prependBucketToHostPath(r *http.Request) error {
	bucket := p.s3.Bucket

	// Extract username for request's url path
	str, err := url.ParseRequestURI(r.URL.Path)
	if err != nil || str.Path == "" {
		return fmt.Errorf("failed to get path from query (%v)", r.URL.Path)
	}
	path := strings.Split(str.Path, "/")
	username := path[1]

	log.Debugf("incoming path: %s", r.URL.Path)
	log.Debugf("incoming raw: %s", r.URL.RawQuery)

	// Restructure request to query the users folder instead of the general bucket
	switch r.Method {
	case http.MethodGet:
		switch {
		case strings.Contains(r.URL.String(), "?uploadId"):
			// resume multipart upload
			r.URL.Path = "/" + bucket + r.URL.Path
		case strings.Contains(r.URL.String(), "?uploads"):
			// list multipart upload
			r.URL.Path = "/" + bucket
			r.URL.RawQuery = "uploads&prefix=" + username + "%2F"
		case strings.Contains(r.URL.String(), "?delimiter"):
			r.URL.Path = "/" + bucket + "/"
			if strings.Contains(r.URL.RawQuery, "&prefix") {
				params := strings.Split(r.URL.RawQuery, "&prefix=")
				r.URL.RawQuery = params[0] + "&prefix=" + username + "%2F" + params[1]
			} else {
				r.URL.RawQuery = r.URL.RawQuery + "&prefix=" + username + "%2F"
			}
			log.Debug("new Raw Query: ", r.URL.RawQuery)
		case strings.Contains(r.URL.String(), "?location") || strings.Contains(r.URL.String(), "&prefix"):
			r.URL.Path = "/" + bucket + "/"
			log.Debug("new Path: ", r.URL.Path)
		}
	case http.MethodPost:
		r.URL.Path = "/" + bucket + r.URL.Path
		log.Debug("new Path: ", r.URL.Path)
	case http.MethodPut:
		r.URL.Path = "/" + bucket + r.URL.Path
		log.Debug("new Path: ", r.URL.Path)
	case http.MethodDelete:
		if strings.Contains(r.URL.String(), "?uploadId") {
			// abort multipart upload
			r.URL.Path = "/" + bucket + r.URL.Path
		}
	}
	log.Infof("User: %v, Request type %v, Path: %v", username, r.Method, r.URL.Path)

	return nil
}

// Function for signing the headers of the s3 requests
// Used for for creating a signature for with the default
// credentials of the s3 service and the user's signature (authentication)
func (p *Proxy) resignHeader(r *http.Request, accessKey string, secretKey string, backendURL string) *http.Request {
	log.Debugf("Generating resigning header for %s", backendURL)
	r.Header.Del("X-Amz-Security-Token")
	r.Header.Del("X-Forwarded-Port")
	r.Header.Del("X-Forwarded-Proto")
	r.Header.Del("X-Forwarded-Host")
	r.Header.Del("X-Forwarded-For")
	r.Header.Del("X-Original-Uri")
	r.Header.Del("X-Real-Ip")
	r.Header.Del("X-Request-Id")
	r.Header.Del("X-Scheme")
	if strings.Contains(backendURL, "//") {
		host := strings.SplitN(backendURL, "//", 2)
		r.Host = host[1]
	}

	return signer.SignV4(*r, accessKey, secretKey, "", p.s3.Region)
}

// Not necessarily a function on the struct since it does not use any of the
// members.
func (p *Proxy) detectRequestType(r *http.Request) S3RequestType {
	switch r.Method {
	case http.MethodGet:
		switch {
		case strings.HasSuffix(r.URL.String(), "/"):
			log.Debug("detect Get")

			return Get
		case strings.Contains(r.URL.String(), "?acl"):
			log.Debug("detect Policy")

			return Policy
		default:
			log.Debug("detect List")

			return List
		}
	case http.MethodDelete:
		switch {
		case strings.HasSuffix(r.URL.String(), "/"):
			log.Debug("detect RemoveBucket")

			return RemoveBucket
		case strings.Contains(r.URL.String(), "uploadId"):
			log.Debug("detect AbortMultipart")

			return AbortMultipart
		default:
			// Do we allow deletion of files?
			log.Debug("detect Delete")

			return Delete
		}
	case http.MethodPut:
		switch {
		case strings.HasSuffix(r.URL.String(), "/"):
			log.Debug("detect MakeBucket")

			return MakeBucket
		case strings.Contains(r.URL.String(), "?policy"):
			log.Debug("detect Policy")

			return Policy
		default:
			// Should decide if we will handle copy here or through authentication
			log.Debug("detect Put")

			return Put
		}
	default:
		log.Debug("detect Other")

		return Other
	}
}

// CreateMessageFromRequest is a function that can take a http request and
// figure out the correct rabbitmq message to send from it.
func (p *Proxy) CreateMessageFromRequest(r *http.Request, claims jwt.Token, user string) (Event, error) {
	event := Event{}
	checksum := Checksum{}
	var err error

	checksum.Value, event.Filesize, err = p.requestInfo(r.URL.Path)
	if err != nil {
		return event, fmt.Errorf("could not get checksum information: %s", err)
	}

	// Case for simple upload
	event.Operation = "upload"
	rawFilepath := strings.Replace(r.URL.Path, "/"+p.s3.Bucket+"/", "", 1)
	event.Filepath = helper.AnonymizeFilepath(rawFilepath, user)

	event.Username = claims.Subject()
	checksum.Type = "sha256"
	event.Checksum = []interface{}{checksum}
	privateClaims := claims.PrivateClaims()
	log.Info("user ", event.Username, " with pilot ", privateClaims["pilot"], " uploaded file ", event.Filepath, " with checksum ", checksum.Value, " at ", time.Now())

	return event, nil
}

// RequestInfo is a function that makes a request to the S3 and collects
// the etag and size information for the uploaded document
func (p *Proxy) requestInfo(fullPath string) (string, int64, error) {
	filePath := strings.Replace(fullPath, "/"+p.s3.Bucket+"/", "", 1)
	client, err := storage.NewS3Client(p.s3)
	if err != nil {
		return "", 0, err
	}

	input := &s3.ListObjectsV2Input{
		Bucket:  &p.s3.Bucket,
		MaxKeys: aws.Int32(1),
		Prefix:  &filePath,
	}

	result, err := client.ListObjectsV2(context.TODO(), input)
	if err != nil {
		log.Debug(err.Error())

		return "", 0, err
	}

	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ReplaceAll(*result.Contents[0].ETag, "\"", "")))), *result.Contents[0].Size, nil

}

// FormatUploadFilePath ensures that path separators are "/", and returns error if the
// filepath contains a disallowed character matched with regex
func formatUploadFilePath(filePath string) (string, error) {
	// Check for mixed "\" and "/" in filepath. Stop and throw an error if true so that
	// we do not end up with unintended folder structure when applying ReplaceAll below
	if strings.Contains(filePath, "\\") && strings.Contains(filePath, "/") {
		return filePath, fmt.Errorf("filepath contains mixed '\\' and '/' characters")
	}

	// make any windows path separators linux compatible
	outPath := strings.ReplaceAll(filePath, "\\", "/")

	// [\x00-\x1F\x7F] is the control character set
	re := regexp.MustCompile(`[\\<>"\|\x00-\x1F\x7F\!\*\'\(\)\;\:\@\&\=\+\$\,\?\%\#\[\]]`)

	disallowedChars := re.FindAllString(outPath, -1)
	if disallowedChars != nil {
		return outPath, fmt.Errorf("filepath contains disallowed characters: %+v", strings.Join(disallowedChars, ", "))
	}

	return outPath, nil
}

// Write the error and its status code to the response
func reportError(errorCode int, message string, w http.ResponseWriter) {
	log.Error(message)
	errorResponse := ErrorResponse{
		Code:    http.StatusText(errorCode),
		Message: message,
	}
	w.WriteHeader(errorCode)
	xmlData, err := xml.Marshal(errorResponse)
	if err != nil {
		// errors are logged but otherwised ignored
		log.Error(err)

		return
	}
	// write the error message to the response
	_, err = io.Writer.Write(w, xmlData)
	if err != nil {
		// errors are logged but otherwised ignored
		log.Error(err)
	}
}
