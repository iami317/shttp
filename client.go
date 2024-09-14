package shttp

import (
	"context"
	"errors"
	"fmt"
	"github.com/iami317/shttp/xtls"
	"golang.org/x/net/http2"
	"golang.org/x/net/publicsuffix"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

type (
	// RequestMiddleware run before request
	RequestMiddleware func(*Request, *Client) error
	// ResponseMiddleware run after receive response
	ResponseMiddleware func(*Response, *Client) error
	// todo 未实现
	// errorHook after retry deal error
	errorHook func(*Request, error)
)

// Client struct
type Client struct {
	HTTPClient    *http.Client
	ClientOptions *ClientOptions
	Debug         bool        // if debug == true, start responseLogger middleware
	Error         interface{} // todo error handle exp
	// todo dns cache
	// Middleware
	defaultBeforeRequest []RequestMiddleware
	extraBeforeRequest   []RequestMiddleware
	afterResponse        []ResponseMiddleware
	errorHooks           []errorHook

	// handle
	LocalAddress    *net.TCPAddr
	closeConnection bool
}

// NewClient xhttp.Client
func NewClient(options *ClientOptions, jar *cookiejar.Jar) (*Client, error) {
	hc, err := createHttpClient(options, false, jar)
	if err != nil {
		return nil, err
	}

	client := createClient(options, hc)
	return client, nil
}

// NewRedirectClient xhttp.Client with Redirect
func NewRedirectClient(options *ClientOptions, jar *cookiejar.Jar) (*Client, error) {
	hc, err := createHttpClient(options, true, jar)
	if err != nil {
		return nil, err
	}

	client := createClient(options, hc)
	return client, nil
}

// NewDefaultClient xhttp.Client not follow redirect
func NewDefaultClient(jar *cookiejar.Jar) (*Client, error) {
	hc, err := createHttpClient(DefaultClientOptions(), false, jar)
	if err != nil {
		return nil, err
	}

	client := createClient(DefaultClientOptions(), hc)
	return client, nil
}

// NewDefaultRedirectClient follow redirect
func NewDefaultRedirectClient(jar *cookiejar.Jar) (*Client, error) {
	hc, err := createHttpClient(DefaultClientOptions(), true, jar)
	if err != nil {
		return nil, err
	}

	client := createClient(DefaultClientOptions(), hc)
	return client, nil
}

// NewWithHTTPClient with http client
func NewWithHTTPClient(options *ClientOptions, hc *http.Client) (*Client, error) {
	return createClient(options, hc), nil
}

// Do request
func (c *Client) Do(ctx context.Context, req *Request) (*Response, error) {
	var (
		resp                 *http.Response
		shouldRetry          bool
		err, doErr, retryErr error
	)
	if c == nil {
		return nil, errors.New("xhttp client not instantiated")
	}

	if c.ClientOptions.SoloConn {
		tlsClientConfig, _ := xtls.NewTLSConfig(c.ClientOptions.TlsOptions)
		c.HTTPClient.Transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				conn, err := net.Dial("tcp", addr)
				if conn != nil {
					c.LocalAddress = conn.LocalAddr().(*net.TCPAddr)
				}
				return conn, err
			},
			MaxConnsPerHost:       c.ClientOptions.MaxConnsPerHost,
			ResponseHeaderTimeout: time.Duration(c.ClientOptions.ReadTimeout) * time.Second,
			IdleConnTimeout:       time.Duration(c.ClientOptions.IdleConnTimeout) * time.Second,
			TLSHandshakeTimeout:   time.Duration(c.ClientOptions.TLSHandshakeTimeout) * time.Second,
			MaxIdleConns:          c.ClientOptions.MaxIdleConns,
			TLSClientConfig:       tlsClientConfig,
			DisableKeepAlives:     c.ClientOptions.DisableKeepAlives,
		}
	}

	req.SetContext(ctx)
	req.attempt = 0

	err = c.ClientOptions.Limiter.Wait(req.GetContext())
	if err != nil {
		return nil, err
	}

	// user diy RequestMiddleware
	for _, f := range c.extraBeforeRequest {
		if err = f(req, c); err != nil {
			return nil, err
		}
	}

	// default diy RequestMiddleware
	for _, f := range c.defaultBeforeRequest {
		if err = f(req, c); err != nil {
			return nil, err
		}
	}
	// do request with retry
	for i := 0; ; i++ {
		req.attempt++

		req.setSendAt()
		resp, doErr = c.HTTPClient.Do(req.RawRequest)
		// need retry
		shouldRetry, retryErr = defaultRetryPolicy(req.GetContext(), resp, doErr)
		if !shouldRetry {
			break
		}

		remain := c.ClientOptions.FailRetries - i
		if remain <= 0 {
			break
		}
		// waitTime
		waitTime := defaultBackoff(defaultRetryWaitMin, defaultRetryWaitMax, i, resp)
		select {
		case <-time.After(waitTime):
		case <-req.GetContext().Done():
			return nil, req.GetContext().Err()
		}
	}

	if doErr == nil && retryErr == nil && !shouldRetry {
		// request success
		//golog.Debugf("request: %s %s, response: status %d content-length %d", req.GetMethod(), req.GetUrl().String(), resp.StatusCode, resp.ContentLength)
		response := &Response{
			Request:     req,
			RawResponse: resp,
		}
		response.setReceivedAt()

		for _, f := range c.afterResponse {
			if err = f(response, c); err != nil {
				return nil, err
			}
		}
		return response, nil
	} else {
		finalErr := doErr
		if retryErr != nil {
			finalErr = retryErr
		}
		//logx.Debugf("%s %s fail", req.GetMethod(), req.GetUrl().String())
		return nil, fmt.Errorf("giving up connect to %s %s after %d attempt(s): %v",
			req.RawRequest.Method, req.RawRequest.URL, req.attempt, finalErr)
	}
}

func (c *Client) BeforeRequest(fn RequestMiddleware) {
	c.extraBeforeRequest = append(c.extraBeforeRequest, fn)
}

func (c *Client) AfterResponse(fn ResponseMiddleware) {
	c.afterResponse = append(c.afterResponse, fn)
}

func (c *Client) SetCloseConnection(close bool) *Client {
	c.closeConnection = close
	return c
}

// 尽最大努力复制
func (c *Client) tryBestClone() *Client {
	newClient := *c
	newHttp := *c.HTTPClient
	newClient.HTTPClient = &newHttp
	newClient.ClientOptions = c.ClientOptions.Clone()
	newClient.defaultBeforeRequest = make([]RequestMiddleware, len(c.defaultBeforeRequest))
	for i, value := range c.defaultBeforeRequest {
		newClient.defaultBeforeRequest[i] = value
	}
	newClient.extraBeforeRequest = make([]RequestMiddleware, len(c.extraBeforeRequest))
	for i, value := range c.extraBeforeRequest {
		newClient.extraBeforeRequest[i] = value
	}
	newClient.afterResponse = make([]ResponseMiddleware, len(c.afterResponse))
	for i, value := range c.afterResponse {
		newClient.afterResponse[i] = value
	}
	newClient.errorHooks = make([]errorHook, len(c.errorHooks))
	for i, value := range c.errorHooks {
		newClient.errorHooks[i] = value
	}
	return &newClient
}

func (c *Client) WithoutCookieJar() *Client {
	newClient := c.tryBestClone()
	newClient.HTTPClient.Jar = nil
	return newClient
}

func (c *Client) WithRedirect(redirect bool) *Client {
	newClient := c.tryBestClone()
	newClient.HTTPClient.CheckRedirect = makeCheckRedirectFunc(redirect, c.ClientOptions.MaxRedirect)
	return newClient
}

func createClient(options *ClientOptions, hc *http.Client) *Client {
	c := &Client{
		HTTPClient:    hc,
		ClientOptions: options,
		Debug:         options.Debug,
	}

	c.extraBeforeRequest = []RequestMiddleware{}
	c.defaultBeforeRequest = []RequestMiddleware{
		verifyRequestMethod,
		createHTTPRequest,
	}
	c.afterResponse = []ResponseMiddleware{
		readResponseBody,
		//responseLogger,
	}
	return c
}

func GetFreePort() int {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func createHttpClient(httpClientOptions *ClientOptions, followRedirects bool, jar *cookiejar.Jar) (*http.Client, error) {
	tlsClientConfig, err := xtls.NewTLSConfig(httpClientOptions.TlsOptions)
	if err != nil {
		return nil, err
	}

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: time.Duration(httpClientOptions.DialTimeout) * time.Second,
		}).DialContext,
		MaxConnsPerHost:       httpClientOptions.MaxConnsPerHost,
		ResponseHeaderTimeout: time.Duration(httpClientOptions.ReadTimeout) * time.Second,
		IdleConnTimeout:       time.Duration(httpClientOptions.IdleConnTimeout) * time.Second,
		TLSHandshakeTimeout:   time.Duration(httpClientOptions.TLSHandshakeTimeout) * time.Second,
		MaxIdleConns:          httpClientOptions.MaxIdleConns,
		TLSClientConfig:       tlsClientConfig,
		DisableKeepAlives:     httpClientOptions.DisableKeepAlives,
	}
	if httpClientOptions.EnableHTTP2 {
		err := http2.ConfigureTransport(transport)
		if err != nil {
			return nil, err
		}
	}

	if httpClientOptions.Proxy != "" {
		proxy, err := url.Parse(httpClientOptions.Proxy)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	// default cookiejar
	if jar == nil {
		cookieJar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
		if err != nil {
			return nil, err
		}
		jar = cookieJar
	}

	return &http.Client{
		Timeout:       time.Duration(httpClientOptions.ReadTimeout+httpClientOptions.DialTimeout) * time.Second,
		Jar:           jar,
		Transport:     transport,
		CheckRedirect: makeCheckRedirectFunc(followRedirects, httpClientOptions.MaxRedirect),
	}, nil
}

type checkRedirectFunc func(req *http.Request, via []*http.Request) error

func makeCheckRedirectFunc(followRedirects bool, maxRedirects int) checkRedirectFunc {
	return func(req *http.Request, via []*http.Request) error {
		if !followRedirects {
			return http.ErrUseLastResponse
		}
		if len(via) >= maxRedirects {
			return http.ErrUseLastResponse
		}
		return nil
	}
}
