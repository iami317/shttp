package shttp

import (
	"fmt"
	"github.com/thoas/go-funk"
	"io"
	"io/ioutil"
	"net/http"
)

//‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾
// Request Middleware(s)
//_______________________________________________________________________

func verifyRequestMethod(req *Request, c *Client) error {
	// req.Method in AllowMethods
	currentMethod := req.RawRequest.Method
	if funk.Contains(c.ClientOptions.AllowMethods, currentMethod) == false {
		return fmt.Errorf(`http method %s not allowed`, currentMethod)
	}
	return nil
}

func createHTTPRequest(req *Request, c *Client) error {
	// enable trace
	if req.trace {
		req.clientTrace = &clientTrace{}
		req.ctx = req.clientTrace.createContext(req.GetContext())
	}
	// assign close connection option
	req.RawRequest.Close = c.closeConnection

	for key, value := range c.ClientOptions.Headers {
		// 如果请求本身有header定义，不要改
		if req.RawRequest.Header.Get(key) == "" {
			req.RawRequest.Header.Set(key, value)
		}
	}
	// add cookie
	if c.ClientOptions.Cookies != nil {
		for k, v := range c.ClientOptions.Cookies {
			req.RawRequest.AddCookie(&http.Cookie{
				Name:  k,
				Value: v,
			})
		}
	}
	// add ctx
	req.RawRequest = req.RawRequest.WithContext(req.GetContext())
	return nil
}

//‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾
// Response Middleware(s)
//_______________________________________________________________________

func readResponseBody(resp *Response, c *Client) error {
	bodyBytes, err := ioutil.ReadAll(io.LimitReader(resp.RawResponse.Body, c.ClientOptions.MaxRespBodySize))
	if err != nil {
		return err
	}
	resp.Body = bodyBytes
	defer resp.RawResponse.Body.Close()
	return nil
}

func responseLogger(resp *Response, c *Client) error {
	if c.Debug {
		req := resp.Request
		reqString, err := req.GetRaw()
		if err != nil {
			return err
		}

		respString, err := resp.GetRaw()
		if err != nil {
			return err
		}

		reqLog := "========= Request ===========\n"
		reqLog += string(reqString) + "\n"
		reqLog += "========= Response ==========\n"
		reqLog += string(respString) + "\n"
		fmt.Println(reqLog)
	}
	return nil
}
