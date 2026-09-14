package objectstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

type Store struct {
	endpoint  *url.URL
	bucket    string
	region    string
	accessKey string
	secretKey string
	client    *http.Client
	ensure    sync.Once
	ensureErr error
}

func NewFromEnv() (*Store, error) {
	endpoint := strings.TrimSpace(os.Getenv("S3_ENDPOINT_URL"))
	bucket := strings.TrimSpace(os.Getenv("S3_BUCKET"))
	accessKey := strings.TrimSpace(os.Getenv("S3_ACCESS_KEY_ID"))
	secretKey := strings.TrimSpace(os.Getenv("S3_SECRET_ACCESS_KEY"))
	if endpoint == "" && bucket == "" && accessKey == "" && secretKey == "" {
		return nil, nil
	}
	if endpoint == "" || bucket == "" || accessKey == "" || secretKey == "" {
		return nil, errors.New("S3_ENDPOINT_URL, S3_BUCKET, S3_ACCESS_KEY_ID and S3_SECRET_ACCESS_KEY must be configured together")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("invalid S3_ENDPOINT_URL")
	}
	region := strings.TrimSpace(os.Getenv("S3_REGION"))
	if region == "" {
		region = "us-east-1"
	}
	return &Store{endpoint: parsed, bucket: bucket, region: region, accessKey: accessKey, secretKey: secretKey, client: &http.Client{Timeout: 60 * time.Second}}, nil
}

func (s *Store) Bucket() string { return s.bucket }

func (s *Store) EnsureBucket(ctx context.Context) error {
	s.ensure.Do(func() {
		status, err := s.do(ctx, http.MethodHead, s.bucket, "", nil, "")
		if err == nil && status >= 200 && status < 300 {
			return
		}
		if err != nil && status == 0 {
			s.ensureErr = err
			return
		}
		if status != http.StatusNotFound {
			s.ensureErr = fmt.Errorf("object storage bucket check returned %d", status)
			return
		}
		status, err = s.do(ctx, http.MethodPut, s.bucket, "", nil, "")
		if err != nil {
			s.ensureErr = err
			return
		}
		if status < 200 || status >= 300 {
			s.ensureErr = fmt.Errorf("create object storage bucket returned %d", status)
		}
	})
	return s.ensureErr
}

func (s *Store) Put(ctx context.Context, key string, content []byte, contentType string) (string, error) {
	if err := s.EnsureBucket(ctx); err != nil {
		return "", err
	}
	resp, err := s.request(ctx, http.MethodPut, s.bucket, key, content, contentType)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("object storage put returned %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	return strings.Trim(resp.Header.Get("ETag"), `"`), nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("object key is required")
	}
	if err := s.EnsureBucket(ctx); err != nil {
		return err
	}
	resp, err := s.request(ctx, http.MethodDelete, s.bucket, key, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil
	}
	message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("object storage delete returned %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	if err := s.EnsureBucket(ctx); err != nil {
		return nil, err
	}
	resp, err := s.request(ctx, http.MethodGet, s.bucket, key, nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("object storage get returned %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 128<<20))
}

func (s *Store) do(ctx context.Context, method, bucket, key string, content []byte, contentType string) (int, error) {
	resp, err := s.request(ctx, method, bucket, key, content, contentType)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, nil
}

func (s *Store) request(ctx context.Context, method, bucket, key string, content []byte, contentType string) (*http.Response, error) {
	canonicalURI := s.canonicalURI(bucket, key)
	requestURL := *s.endpoint
	requestURL.Path = canonicalURI
	requestURL.RawPath = ""
	requestURL.RawQuery = ""
	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	now := time.Now().UTC()
	amzDate, dateStamp := now.Format("20060102T150405Z"), now.Format("20060102")
	payloadHash := sha256Hex(content)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("x-amz-date", amzDate)
	headers := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	canonicalHeaders := "host:" + req.URL.Host + "\nx-amz-content-sha256:" + payloadHash + "\nx-amz-date:" + amzDate + "\n"
	if contentType != "" {
		headers = append(headers, "content-type")
		canonicalHeaders = "content-type:" + strings.TrimSpace(contentType) + "\n" + canonicalHeaders
	}
	sort.Strings(headers)
	canonicalRequest := strings.Join([]string{method, canonicalURI, "", canonicalHeaders, strings.Join(headers, ";"), payloadHash}, "\n")
	scope := dateStamp + "/" + s.region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))
	signature := hex.EncodeToString(hmacSHA256(signingKey(s.secretKey, dateStamp, s.region, "s3"), []byte(stringToSign)))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.accessKey+"/"+scope+", SignedHeaders="+strings.Join(headers, ";")+", Signature="+signature)
	return s.client.Do(req)
}

func (s *Store) canonicalURI(bucket, key string) string {
	base := strings.TrimSuffix(s.endpoint.Path, "/")
	parts := []string{base, escapeSegment(bucket)}
	if strings.Trim(key, "/") != "" {
		for _, segment := range strings.Split(strings.Trim(key, "/"), "/") {
			parts = append(parts, escapeSegment(segment))
		}
	}
	return path.Clean(strings.Join(parts, "/"))
}

func escapeSegment(value string) string {
	return strings.ReplaceAll(url.PathEscape(value), "+", "%20")
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func signingKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func hmacSHA256(key, content []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(content)
	return mac.Sum(nil)
}
