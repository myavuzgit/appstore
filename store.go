package appstore

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const (
	HostSandBox    = "https://api.storekit-sandbox.itunes.apple.com"
	HostProduction = "https://api.storekit.itunes.apple.com"

	PathLookUp                        = "/inApps/v1/lookup/{orderId}"
	PathTransactionHistory            = "/inApps/v1/history/{originalTransactionId}"
	PathRefundHistory                 = "/inApps/v2/refund/lookup/{originalTransactionId}"
	PathGetALLSubscriptionStatus      = "/inApps/v1/subscriptions/{originalTransactionId}"
	PathConsumptionInfo               = "/inApps/v1/transactions/consumption/{originalTransactionId}"
	PathExtendSubscriptionRenewalDate = "/inApps/v1/subscriptions/extend/{originalTransactionId}"
	PathGetNotificationHistory        = "/inApps/v1/notifications/history"
	PathRequestTestNotification       = "/inApps/v1/notifications/test"
	PathGetTestNotificationStatus     = "/inApps/v1/notifications/test/{testNotificationToken}"
)

type StoreConfig struct {
	KeyContent []byte // Loads a .p8 certificate
	KeyID      string // Your private key ID from App Store Connect (Ex: 2X9R4HXF34)
	BundleID   string // Your app’s bundle ID
	Issuer     string // Your issuer ID from the Keys page in App Store Connect (Ex: "57246542-96fe-1a63-e053-0824d011072a")
	Sandbox    bool   // default is Production
}

type StoreClient struct {
	Token   *Token
	httpCli *http.Client
	cert    *Cert
	hostUrl string
}

// NewStoreClient create a appstore server api client
func NewStoreClient(config *StoreConfig) *StoreClient {
	token := &Token{}
	token.WithConfig(config)
	hostUrl := HostProduction
	if config.Sandbox {
		hostUrl = HostSandBox
	}

	client := &StoreClient{
		Token: token,
		cert:  &Cert{},
		httpCli: &http.Client{
			Timeout: 30 * time.Second,
		},
		hostUrl: hostUrl,
	}
	return client
}

// NewStoreClientWithHTTPClient creates a appstore server api client with a custom http client.
func NewStoreClientWithHTTPClient(config *StoreConfig, httpClient *http.Client) *StoreClient {
	token := &Token{}
	token.WithConfig(config)
	hostUrl := HostProduction
	if config.Sandbox {
		hostUrl = HostSandBox
	}

	client := &StoreClient{
		Token:   token,
		cert:    &Cert{},
		httpCli: httpClient,
		hostUrl: hostUrl,
	}
	return client
}

func (c *StoreClient) initHttpClient(hc HTTPClient) (DoFunc, error) {
	authToken, err := c.Token.GenerateIfExpired()
	if err != nil {
		return nil, fmt.Errorf("appstore generate token err %w", err)
	}
	return AddHeader(hc, "Authorization", "Bearer "+authToken), nil
}

// GetALLSubscriptionStatuses https://developer.apple.com/documentation/appstoreserverapi/get_all_subscription_statuses
func (c *StoreClient) GetALLSubscriptionStatuses(ctx context.Context, originalTransactionId string) (*StatusResponse, error) {
	URL := c.hostUrl + PathGetALLSubscriptionStatus
	URL = strings.Replace(URL, "{originalTransactionId}", originalTransactionId, -1)

	var client HTTPClient
	client = c.httpCli
	client = SetInitializer(client, c.initHttpClient)
	client = RequireResponseStatus(client, http.StatusOK)
	client = SetRequest(ctx, client, http.MethodGet, URL)
	rsp := &StatusResponse{}
	client = SetResponseBodyHandler(client, json.Unmarshal, rsp)

	_, err := client.Do(nil)
	if err != nil {
		return nil, err
	}
	return rsp, nil
}

// LookupOrderID https://developer.apple.com/documentation/appstoreserverapi/look_up_order_id
func (c *StoreClient) LookupOrderID(ctx context.Context, orderId string) (*OrderLookupResponse, error) {
	URL := c.hostUrl + PathLookUp
	URL = strings.Replace(URL, "{orderId}", orderId, -1)

	var client HTTPClient
	client = c.httpCli
	client = SetInitializer(client, c.initHttpClient)
	client = RequireResponseStatus(client, http.StatusOK)
	client = SetRequest(ctx, client, http.MethodGet, URL)
	rsp := &OrderLookupResponse{}
	client = SetResponseBodyHandler(client, json.Unmarshal, rsp)

	_, err := client.Do(nil)
	if err != nil {
		return nil, err
	}
	return rsp, nil
}

// GetTransactionHistory https://developer.apple.com/documentation/appstoreserverapi/get_transaction_history
func (c *StoreClient) GetTransactionHistory(ctx context.Context, originalTransactionId string, query *url.Values) (responses []*HistoryResponse, err error) {
	URL := c.hostUrl + PathTransactionHistory
	URL = strings.Replace(URL, "{originalTransactionId}", originalTransactionId, -1)

	if query == nil {
		query = &url.Values{}
	}

	var client HTTPClient
	client = c.httpCli
	client = SetInitializer(client, c.initHttpClient)
	client = RequireResponseStatus(client, http.StatusOK)

	for {
		rsp := HistoryResponse{}
		client = SetRequest(ctx, client, http.MethodGet, URL+"?"+query.Encode())
		client = SetResponseBodyHandler(client, json.Unmarshal, rsp)
		_, err = client.Do(nil)
		if err != nil {
			return nil, err
		}

		responses = append(responses, &rsp)
		if !rsp.HasMore {
			return
		}

		if rsp.Revision != "" {
			query.Set("revision", rsp.Revision)
		} else {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// GetRefundHistory https://developer.apple.com/documentation/appstoreserverapi/get_refund_history
func (c *StoreClient) GetRefundHistory(ctx context.Context, originalTransactionId string) (responses []*RefundLookupResponse, err error) {
	baseURL := c.hostUrl + PathRefundHistory
	baseURL = strings.Replace(baseURL, "{originalTransactionId}", originalTransactionId, -1)

	URL := baseURL
	var client HTTPClient
	client = c.httpCli
	client = SetInitializer(client, c.initHttpClient)
	client = RequireResponseStatus(client, http.StatusOK)

	for {
		rsp := RefundLookupResponse{}
		client = SetRequest(ctx, client, http.MethodGet, URL)
		client = SetResponseBodyHandler(client, json.Unmarshal, rsp)
		_, err = client.Do(nil)
		if err != nil {
			return nil, err
		}

		responses = append(responses, &rsp)
		if !rsp.HasMore {
			return
		}

		data := url.Values{}
		if rsp.Revision != "" {
			data.Set("revision", rsp.Revision)
			URL = baseURL + "?" + data.Encode()
		} else {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// SendConsumptionInfo https://developer.apple.com/documentation/appstoreserverapi/send_consumption_information
func (c *StoreClient) SendConsumptionInfo(ctx context.Context, originalTransactionId string, body ConsumptionRequestBody) (statusCode int, err error) {
	URL := c.hostUrl + PathConsumptionInfo
	URL = strings.Replace(URL, "{originalTransactionId}", originalTransactionId, -1)

	bodyBuf := new(bytes.Buffer)
	err = json.NewEncoder(bodyBuf).Encode(body)
	if err != nil {
		return 0, err
	}

	statusCode, _, err = c.Do(ctx, http.MethodPut, URL, bodyBuf)
	if err != nil {
		return statusCode, err
	}
	return statusCode, nil
}

// ExtendSubscriptionRenewalDate https://developer.apple.com/documentation/appstoreserverapi/extend_a_subscription_renewal_date
func (c *StoreClient) ExtendSubscriptionRenewalDate(ctx context.Context, originalTransactionId string, body ExtendRenewalDateRequest) (statusCode int, err error) {
	URL := c.hostUrl + PathExtendSubscriptionRenewalDate
	URL = strings.Replace(URL, "{originalTransactionId}", originalTransactionId, -1)

	bodyBuf := new(bytes.Buffer)
	err = json.NewEncoder(bodyBuf).Encode(body)
	if err != nil {
		return 0, err
	}

	statusCode, _, err = c.Do(ctx, http.MethodPut, URL, bodyBuf)
	if err != nil {
		return statusCode, err
	}
	return statusCode, nil
}

// GetNotificationHistory https://developer.apple.com/documentation/appstoreserverapi/get_notification_history
func (c *StoreClient) GetNotificationHistory(ctx context.Context, body NotificationHistoryRequest) (responses []NotificationHistoryResponseItem, err error) {
	baseURL := c.hostUrl + PathGetNotificationHistory

	bodyBuf := new(bytes.Buffer)
	err = json.NewEncoder(bodyBuf).Encode(body)
	if err != nil {
		return nil, err
	}

	URL := baseURL
	var client HTTPClient
	client = c.httpCli
	client = SetInitializer(client, c.initHttpClient)
	client = RequireResponseStatus(client, http.StatusOK)

	for {
		rsp := NotificationHistoryResponses{}
		rsp.NotificationHistory = make([]NotificationHistoryResponseItem, 0)

		client = SetRequest(ctx, client, http.MethodPost, URL)
		client = SetRequestBodyJSON(client, bodyBuf)
		client = SetResponseBodyHandler(client, json.Unmarshal, rsp)
		_, err = client.Do(nil)
		if err != nil {
			return nil, err
		}

		responses = append(responses, rsp.NotificationHistory...)
		if !rsp.HasMore {
			return responses, nil
		}

		data := url.Values{}
		if rsp.PaginationToken != "" {
			data.Set("paginationToken", rsp.PaginationToken)
			URL = baseURL + "?" + data.Encode()
		} else {
			return responses, nil
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// SendRequestTestNotification https://developer.apple.com/documentation/appstoreserverapi/request_a_test_notification
func (c *StoreClient) SendRequestTestNotification(ctx context.Context) (int, []byte, error) {
	URL := c.hostUrl + PathRequestTestNotification

	return c.Do(ctx, http.MethodPost, URL, nil)
}

// GetTestNotificationStatus https://developer.apple.com/documentation/appstoreserverapi/get_test_notification_status
func (c *StoreClient) GetTestNotificationStatus(ctx context.Context, testNotificationToken string) (int, []byte, error) {
	URL := c.hostUrl + PathGetTestNotificationStatus
	URL = strings.Replace(URL, "{testNotificationToken}", testNotificationToken, -1)

	return c.Do(ctx, http.MethodGet, URL, nil)
}

// ParseSignedTransactions parse the jws singed transactions
// Per doc: https://datatracker.ietf.org/doc/html/rfc7515#section-4.1.6
func (c *StoreClient) ParseSignedTransactions(transactions []string) ([]*JWSTransaction, error) {
	result := make([]*JWSTransaction, 0)
	for _, v := range transactions {
		trans, err := c.parseSignedTransaction(v)
		if err == nil && trans != nil {
			result = append(result, trans)
		}
	}

	return result, nil
}

func (c *StoreClient) parseSignedTransaction(transaction string) (*JWSTransaction, error) {
	tran := &JWSTransaction{}

	rootCertBytes, err := c.cert.extractCertByIndex(transaction, 2)
	if err != nil {
		return nil, err
	}
	rootCert, err := x509.ParseCertificate(rootCertBytes)
	if err != nil {
		return nil, errors.New("failed to parse root certificate")
	}
	intermediaCertBytes, err := c.cert.extractCertByIndex(transaction, 1)
	if err != nil {
		return nil, err
	}
	intermediaCert, err := x509.ParseCertificate(intermediaCertBytes)
	if err != nil {
		return nil, errors.New("failed to parse intermediate certificate")
	}
	leafCertBytes, err := c.cert.extractCertByIndex(transaction, 0)
	if err != nil {
		return nil, err
	}
	leafCert, err := x509.ParseCertificate(leafCertBytes)
	if err != nil {
		return nil, errors.New("failed to parse leaf certificate")
	}
	if err = c.cert.verifyCert(rootCert, intermediaCert, leafCert); err != nil {
		return nil, err
	}

	pk, ok := leafCert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("appstore public key must be of type ecdsa.PublicKey")
	}

	_, err = jwt.ParseWithClaims(transaction, tran, func(token *jwt.Token) (interface{}, error) {
		return pk, nil
	})
	if err != nil {
		return nil, err
	}

	return tran, nil
}

// Per doc: https://developer.apple.com/documentation/appstoreserverapi#topics
func (c *StoreClient) Do(ctx context.Context, method string, url string, body io.Reader) (int, []byte, error) {
	authToken, err := c.Token.GenerateIfExpired()
	if err != nil {
		return 0, nil, fmt.Errorf("appstore generate token err %w", err)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return 0, nil, fmt.Errorf("appstore new http request err %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("User-Agent", "App Store Client")
	req = req.WithContext(ctx)

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("appstore http client do err %w", err)
	}
	defer resp.Body.Close()

	byteData, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("appstore read http body err %w", err)
	}

	return resp.StatusCode, byteData, err
}