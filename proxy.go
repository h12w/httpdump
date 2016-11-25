package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"net/http/httputil"

	"appcoachs.net/x/log"
	"h12.me/config"
	"h12.me/errors"
	"h12.me/mitm"
)

// Config is global config struct
type Config struct {
	Port  int    `long:"port" description:"listening port" default:"2080"`
	Proxy string `long:"proxy" description:"http proxy"`
}

func main() {
	var cfg Config
	if err := config.Parse(&cfg); err != nil {
		if _, ok := err.(*config.HelpError); ok {
			fmt.Println(err)
			return
		}
		log.Fatal(err)
	}
	certs, err := mitm.NewCertPool("cert")
	if err != nil {
		log.Fatal(err)
	}

	client := http.Client{}
	if cfg.Proxy != "" {
		upstreamProxy, err := url.Parse(cfg.Proxy)
		if err != nil {
			log.Fatal(err)
		}
		client.Transport = &http.Transport{
			// TEST: for local testing
			// Dial: socks.DialSocksProxy(socks.SOCKS5, "127.0.0.1:1080"),
			Proxy: http.ProxyURL(upstreamProxy),
			Dial: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}

	proxy := newProxy(certs, &client)
	http.ListenAndServe(":"+strconv.Itoa(cfg.Port), http.HandlerFunc(proxy.serve))
}

type proxy struct {
	certs  *mitm.CertPool
	client *http.Client
}

func newProxy(certs *mitm.CertPool, client *http.Client) *proxy {
	fp := &proxy{
		certs:  certs,
		client: client,
	}

	return fp
}

func (p *proxy) serve(w http.ResponseWriter, req *http.Request) {
	if req.Method == "GET" {
		p.serveHTTP(w, req)
	} else if req.Method == "CONNECT" {
		err := p.certs.ServeHTTPS(w, req, p.serveHTTP)
		if err != nil {
			log.Error(errors.Wrap(err))
		}
	}
}

func (p *proxy) serveHTTP(w http.ResponseWriter, req *http.Request) {
	req.RequestURI = ""
	resp, err := p.client.Do(req)
	if err != nil {
		log.Error(errors.Wrap(err))
		return
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Encoding") == "gzip" {
		body, err := newGzipReadCloser(resp.Body)
		if err == nil {
			resp.Body = body
			resp.ContentLength = 0
		}
	}
	fmt.Println("----- ------ ----- ----- ----- -----")
	buf, _ := httputil.DumpRequest(req, true)
	fmt.Println(string(buf))
	buf, _ = httputil.DumpResponse(resp, true)
	fmt.Println(string(buf))
	fmt.Println("")
	fmt.Println("")

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Error(errors.Wrap(err))
	}
}

type gzipReadCloser struct {
	rc io.ReadCloser
	*gzip.Reader
}

func newGzipReadCloser(rc io.ReadCloser) (*gzipReadCloser, error) {
	reader, err := gzip.NewReader(rc)
	if err != nil {
		return nil, err
	}
	return &gzipReadCloser{
		rc:     rc,
		Reader: reader,
	}, nil
}

func (r *gzipReadCloser) Close() error {
	if err := r.rc.Close(); err != nil {
		return err
	}
	return r.Reader.Close()
}
