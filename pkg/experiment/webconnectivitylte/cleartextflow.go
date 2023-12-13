package webconnectivitylte

//
// CleartextFlow
//
// Generated by `boilerplate' using the http template.
//

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ooni/probe-engine/pkg/logx"
	"github.com/ooni/probe-engine/pkg/measurexlite"
	"github.com/ooni/probe-engine/pkg/model"
	"github.com/ooni/probe-engine/pkg/netxlite"
	"github.com/ooni/probe-engine/pkg/throttling"
)

// Measures HTTP endpoints.
//
// The zero value of this structure IS NOT valid and you MUST initialize
// all the fields marked as MANDATORY before using this structure.
type CleartextFlow struct {
	// Address is the MANDATORY address to connect to.
	Address string

	// DNSCache is the MANDATORY DNS cache.
	DNSCache *DNSCache

	// Depth is the OPTIONAL current redirect depth.
	Depth int64

	// IDGenerator is the MANDATORY atomic int64 to generate task IDs.
	IDGenerator *atomic.Int64

	// Logger is the MANDATORY logger to use.
	Logger model.Logger

	// NumRedirects it the MANDATORY counter of the number of redirects.
	NumRedirects *NumRedirects

	// TestKeys is MANDATORY and contains the TestKeys.
	TestKeys *TestKeys

	// ZeroTime is the MANDATORY measurement's zero time.
	ZeroTime time.Time

	// WaitGroup is the MANDATORY wait group this task belongs to.
	WaitGroup *sync.WaitGroup

	// CookieJar contains the OPTIONAL cookie jar, used for redirects.
	CookieJar http.CookieJar

	// FollowRedirects is OPTIONAL and instructs this flow
	// to follow HTTP redirects (if any).
	FollowRedirects bool

	// HostHeader is the OPTIONAL host header to use.
	HostHeader string

	// PrioSelector is the OPTIONAL priority selector to use to determine
	// whether this flow is allowed to fetch the webpage.
	PrioSelector *prioritySelector

	// Referer contains the OPTIONAL referer, used for redirects.
	Referer string

	// UDPAddress is the OPTIONAL address of the UDP resolver to use. If this
	// field is not set we use a default one (e.g., `8.8.8.8:53`).
	UDPAddress string

	// URLPath is the OPTIONAL URL path.
	URLPath string

	// URLRawQuery is the OPTIONAL URL raw query.
	URLRawQuery string
}

// Start starts this task in a background goroutine.
func (t *CleartextFlow) Start(ctx context.Context) {
	t.WaitGroup.Add(1)
	index := t.IDGenerator.Add(1)
	go func() {
		defer t.WaitGroup.Done() // synchronize with the parent
		t.Run(ctx, index)
	}()
}

// Run runs this task in the current goroutine.
func (t *CleartextFlow) Run(parentCtx context.Context, index int64) error {
	if err := allowedToConnect(t.Address); err != nil {
		t.Logger.Warnf("CleartextFlow: %s", err.Error())
		return err
	}

	// create trace
	trace := measurexlite.NewTrace(index, t.ZeroTime, fmt.Sprintf("depth=%d", t.Depth),
		fmt.Sprintf("fetch_body=%v", t.PrioSelector != nil))

	// TODO(bassosimone): the DSL starts measuring for throttling when we start
	// fetching the body while here we start immediately. We should come up with
	// a consistent policy otherwise data won't be comparable.

	// start measuring throttling
	sampler := throttling.NewSampler(trace)
	defer func() {
		t.TestKeys.AppendNetworkEvents(sampler.ExtractSamples()...)
		sampler.Close()
	}()

	// start the operation logger
	ol := logx.NewOperationLogger(
		t.Logger, "[#%d] GET http://%s using %s", index, t.HostHeader, t.Address,
	)

	// perform the TCP connect
	const tcpTimeout = 10 * time.Second
	tcpCtx, tcpCancel := context.WithTimeout(parentCtx, tcpTimeout)
	defer tcpCancel()
	tcpDialer := trace.NewDialerWithoutResolver(t.Logger)
	tcpConn, err := tcpDialer.DialContext(tcpCtx, "tcp", t.Address)
	t.TestKeys.AppendTCPConnectResults(trace.TCPConnects()...)
	defer t.TestKeys.AppendNetworkEvents(trace.NetworkEvents()...) // here to include "connect" events
	if err != nil {
		ol.Stop(err)
		return err
	}
	defer tcpConn.Close()

	alpn := "" // no ALPN because we're not using TLS

	// Determine whether we're allowed to fetch the webpage
	if t.PrioSelector == nil || !t.PrioSelector.permissionToFetch(t.Address) {
		ol.Stop("stop after TCP connect")
		return errNotPermittedToFetch
	}

	// create HTTP transport
	// TODO(https://github.com/ooni/probe/issues/2534): here we're using the QUIRKY netxlite.NewHTTPTransport
	// function, but we can probably avoid using it, given that this code is
	// not using tracing and does not care about those quirks.
	httpTransport := netxlite.NewHTTPTransport(
		t.Logger,
		netxlite.NewSingleUseDialer(tcpConn),
		netxlite.NewNullTLSDialer(),
	)

	// create HTTP request
	const httpTimeout = 10 * time.Second
	httpCtx, httpCancel := context.WithTimeout(parentCtx, httpTimeout)
	defer httpCancel()
	httpReq, err := t.newHTTPRequest(httpCtx)
	if err != nil {
		if t.Referer == "" {
			// when the referer is empty, the failing URL comes from our backend
			// or from the user, so it's a fundamental failure. After that, we
			// are dealing with websites provided URLs, so we should not flag a
			// fundamental failure, because we want to see the measurement submitted.
			t.TestKeys.SetFundamentalFailure(err)
		}
		ol.Stop(err)
		return err
	}

	// perform HTTP transaction
	httpResp, httpRespBody, err := t.httpTransaction(
		httpCtx,
		"tcp",
		t.Address,
		alpn,
		httpTransport,
		httpReq,
		trace,
	)
	if err != nil {
		ol.Stop(err)
		return err
	}

	// if enabled, follow possible redirects
	t.maybeFollowRedirects(parentCtx, httpResp)

	// TODO: insert here additional code if needed
	_ = httpRespBody

	// completed successfully
	ol.Stop(nil)
	return nil
}

// urlHost computes the host to include into the URL
func (t *CleartextFlow) urlHost(scheme string) (string, error) {
	addr, port, err := net.SplitHostPort(t.Address)
	if err != nil {
		t.Logger.Warnf("BUG: net.SplitHostPort failed for %s: %s", t.Address, err.Error())
		return "", err
	}
	urlHost := t.HostHeader
	if urlHost == "" {
		urlHost = addr
	}
	if port == "80" && scheme == "http" {
		return urlHost, nil
	}
	urlHost = net.JoinHostPort(urlHost, port)
	return urlHost, nil
}

// newHTTPRequest creates a new HTTP request.
func (t *CleartextFlow) newHTTPRequest(ctx context.Context) (*http.Request, error) {
	const urlScheme = "http"
	urlHost, err := t.urlHost(urlScheme)
	if err != nil {
		return nil, err
	}
	httpURL := &url.URL{
		Scheme:   urlScheme,
		Host:     urlHost,
		Path:     t.URLPath,
		RawQuery: t.URLRawQuery,
	}
	httpReq, err := http.NewRequestWithContext(ctx, "GET", httpURL.String(), nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Host", t.HostHeader)
	httpReq.Header.Set("Accept", model.HTTPHeaderAccept)
	httpReq.Header.Set("Accept-Language", model.HTTPHeaderAcceptLanguage)
	httpReq.Header.Set("Referer", t.Referer)
	httpReq.Header.Set("User-Agent", model.HTTPHeaderUserAgent)
	httpReq.Host = t.HostHeader
	if t.CookieJar != nil {
		for _, cookie := range t.CookieJar.Cookies(httpURL) {
			httpReq.AddCookie(cookie)
		}
	}
	return httpReq, nil
}

// httpTransaction runs the HTTP transaction and saves the results.
func (t *CleartextFlow) httpTransaction(ctx context.Context, network, address, alpn string,
	txp model.HTTPTransport, req *http.Request, trace *measurexlite.Trace) (*http.Response, []byte, error) {
	const maxbody = 1 << 19
	started := trace.TimeSince(trace.ZeroTime())
	// TODO(bassosimone): I am wondering whether we should have the HTTP transaction
	// start at the beginning of the flow rather than here. If we start it at the
	// beginning this is nicer, but, at the same time, starting it at the beginning
	// of the flow means we're not collecting information about DNS. So, I am a
	// bit torn about what is the best approach to follow here. Maybe it does not
	// even matter to emit transaction_start/end events given that we have transaction ID.
	t.TestKeys.AppendNetworkEvents(measurexlite.NewAnnotationArchivalNetworkEvent(
		trace.Index(), started, "http_transaction_start", trace.Tags()...,
	))
	resp, err := txp.RoundTrip(req)
	var body []byte
	if err == nil {
		defer resp.Body.Close()
		if cookies := resp.Cookies(); t.CookieJar != nil && len(cookies) > 0 {
			t.CookieJar.SetCookies(req.URL, cookies)
		}
		reader := io.LimitReader(resp.Body, maxbody)
		body, err = StreamAllContext(ctx, reader)
	}
	finished := trace.TimeSince(trace.ZeroTime())
	t.TestKeys.AppendNetworkEvents(measurexlite.NewAnnotationArchivalNetworkEvent(
		trace.Index(), finished, "http_transaction_done", trace.Tags()...,
	))
	ev := measurexlite.NewArchivalHTTPRequestResult(
		trace.Index(),
		started,
		network,
		address,
		alpn,
		txp.Network(),
		req,
		resp,
		maxbody,
		body,
		err,
		finished,
		trace.Tags()...,
	)
	t.TestKeys.PrependRequests(ev)
	return resp, body, err
}

// maybeFollowRedirects follows redirects if configured and needed
func (t *CleartextFlow) maybeFollowRedirects(ctx context.Context, resp *http.Response) {
	if !t.FollowRedirects || !t.NumRedirects.CanFollowOneMoreRedirect() {
		return // not configured or too many redirects
	}
	switch resp.StatusCode {
	case 301, 302, 307, 308:
		location, err := resp.Location()
		if err != nil {
			return // broken response from server
		}
		// TODO(https://github.com/ooni/probe/issues/2628): we need to handle
		// the case where the redirect URL is incomplete
		t.Logger.Infof("redirect to: %s", location.String())
		resolvers := &DNSResolvers{
			CookieJar:    t.CookieJar,
			Depth:        t.Depth + 1,
			DNSCache:     t.DNSCache,
			Domain:       location.Hostname(),
			IDGenerator:  t.IDGenerator,
			Logger:       t.Logger,
			NumRedirects: t.NumRedirects,
			TestKeys:     t.TestKeys,
			URL:          location,
			ZeroTime:     t.ZeroTime,
			WaitGroup:    t.WaitGroup,
			Referer:      resp.Request.URL.String(),
			Session:      nil, // no need to issue another control request
			TestHelpers:  nil, // ditto
			UDPAddress:   t.UDPAddress,
		}
		resolvers.Start(ctx)
	default:
		// no redirect to follow
	}
}
