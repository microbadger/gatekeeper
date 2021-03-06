package core

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/jonmorehouse/gatekeeper/gatekeeper"
	"github.com/tylerb/graceful"
)

type Server interface {
	starter
	gracefulStopper
}

func NewHTTPServer(protocol gatekeeper.Protocol, port uint, router RouterClient, lb LoadBalancerClient, modifier ModifierClient, proxier Proxier, metricWriter MetricWriterClient) Server {
	mux := http.NewServeMux()

	instance := &server{
		protocol: protocol,
		port:     port,

		router:       router,
		loadBalancer: lb,
		modifier:     modifier,
		metricWriter: metricWriter,
		proxier:      proxier,

		stopCh: make(chan struct{}, 1),
		errCh:  make(chan error, 1),

		httpServer: &graceful.Server{
			Server: &http.Server{
				Addr:    fmt.Sprintf(":%d", port),
				Handler: mux,
			},
			NoSignalHandling: true,
		},
	}
	mux.HandleFunc("/", instance.httpHandler)
	return instance
}

func NewHTTPSServer(protocol gatekeeper.Protocol, port uint, router RouterClient, lb LoadBalancerClient, modifier ModifierClient, proxier Proxier, metricWriter MetricWriterClient) Server {
	mux := http.NewServeMux()

	instance := &server{
		protocol: protocol,
		port:     port,

		router:       router,
		loadBalancer: lb,
		modifier:     modifier,
		metricWriter: metricWriter,
		proxier:      proxier,

		stopCh: make(chan struct{}, 1),
		errCh:  make(chan error, 1),

		httpServer: &graceful.Server{
			Server: &http.Server{
				Addr:    fmt.Sprintf(":%d", port),
				Handler: mux,
			},
			NoSignalHandling: true,
		},
	}

	mux.HandleFunc("/", instance.httpHandler)
	return instance
}

type server struct {
	protocol gatekeeper.Protocol
	port     uint

	router       RouterClient
	loadBalancer LoadBalancerClient
	modifier     ModifierClient
	metricWriter MetricWriterClient
	proxier      Proxier

	stopAccepting bool
	stopCh        chan struct{}
	errCh         chan error

	httpServer *graceful.Server

	SyncStartStopper
	sync.Mutex
}

func (s *server) Start() error {
	return s.SyncStart(func() error {
		// start the metric worker for tracking current requests
		s.eventMetric(gatekeeper.ServerStartedEvent)
		return s.startHTTP()
	})
}

func (s *server) Stop(duration time.Duration) error {
	return s.SyncStop(func() error {
		s.eventMetric(gatekeeper.ServerStoppedEvent)
		s.httpServer.Stop(duration)
		s.stopCh <- struct{}{}
		return <-s.errCh
	})
}

func (s *server) startHTTP() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.httpHandler)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	s.httpServer = &graceful.Server{
		Server:           server,
		NoSignalHandling: true,
	}

	// the errCh is responsible for emitting an error when the server fails or closes.
	errCh := make(chan error, 1)

	// start the server in a goroutine, passing any errors back to the errCh
	go func() {
		log.Println("listening on port: ", s.port)
		err := s.httpServer.ListenAndServe()
		errCh <- err
	}()

	for {
		select {
		case err := <-errCh:
			return err
		case <-time.After(time.Millisecond * 100):
			goto finished
		}
	}

finished:
	s.errCh = errCh
	return nil
}

func (s *server) httpHandler(rw http.ResponseWriter, rawReq *http.Request) {
	start := time.Now()
	req := gatekeeper.NewRequest(rawReq, s.protocol)

	metric := &gatekeeper.RequestMetric{
		Request:        req,
		RequestStartTS: start,
	}

	s.eventMetric(gatekeeper.RequestAcceptedEvent)

	// finish the request metric, and emit it to the MetricWriter at the
	// end of this function, after the response has been written
	defer func(metric *gatekeeper.RequestMetric) {
		metric.Timestamp = time.Now()
		metric.RequestEndTS = time.Now()
		metric.Latency = time.Now().Sub(start)
		metric.InternalLatency = metric.Latency - metric.ProxyLatency
		//metric.ResponseType = gatekeeper.NewResponseType(metric.Response.StatusCode)

		// emit the request metric to the MetricWriter
		s.metricWriter.RequestMetric(metric)
	}(metric)

	if s.stopAccepting {
		resp := gatekeeper.NewErrorResponse(500, ServerShuttingDownError)
		metric.Response = resp
		metric.Error = gatekeeper.NewError(ServerShuttingDownError)
		s.writeError(rw, ServerShuttingDownError, req, resp)
		return
	}

	// build a *gatekeeper.Request for this rawReq; a wrapper with additional
	// meta information around an *http.Request object
	matchStartTS := time.Now()
	upstream, req, err := s.router.RouteRequest(req)
	if err != nil {
		resp := gatekeeper.NewErrorResponse(400, err)
		metric.Response = resp
		metric.Error = gatekeeper.NewError(err)
		s.writeError(rw, err, req, resp)
		return
	}
	metric.RouterLatency = time.Now().Sub(matchStartTS)
	metric.Upstream = upstream

	// fetch a backend from the loadbalancer to proxy this request too
	loadBalancerStartTS := time.Now()
	backend, err := s.loadBalancer.GetBackend(upstream.ID)
	if err != nil {
		resp := gatekeeper.NewErrorResponse(500, err)
		metric.Response = resp
		metric.Error = gatekeeper.NewError(err)
		s.writeError(rw, err, req, resp)
		return
	}
	metric.LoadBalancerLatency = time.Now().Sub(loadBalancerStartTS)
	metric.Backend = backend

	modifierStartTS := time.Now()
	req, err = s.modifier.ModifyRequest(req)
	if err != nil {
		log.Println(err)
		resp := gatekeeper.NewErrorResponse(500, err)
		metric.Error = gatekeeper.NewError(err)
		metric.Response = resp
		s.writeError(rw, err, req, resp)
		return
	}
	metric.RequestModifierLatency = time.Now().Sub(modifierStartTS)

	if req.Error != nil {
		resp := gatekeeper.NewErrorResponse(500, err)
		metric.Error = req.Error
		metric.Response = resp
		s.writeError(rw, err, req, resp)
		return
	}

	if req.Response != nil {
		metric.Response = req.Response
		s.writeResponse(rw, req.Response)
		return
	}

	// the proxier will only return an error when it is having an internal
	// problem and was unable to even start the proxy cycle. Any sort of
	// proxy error in the proxy lifecycle is handled internally, due to the
	// coupling that is required with the internal go httputil.ReverseProxy
	// and http.Transport types
	if err := s.proxier.Proxy(rw, rawReq, req, upstream, backend, metric); err != nil {
		resp := gatekeeper.NewErrorResponse(500, err)
		metric.Response = resp
		metric.Error = gatekeeper.NewError(err)
		s.writeError(rw, err, req, resp)
		return
	}

	s.eventMetric(gatekeeper.RequestSuccessEvent)
}

// write an error response, calling the ErrorResponse handler in the modifier plugin
func (s *server) writeError(rw http.ResponseWriter, err error, request *gatekeeper.Request, response *gatekeeper.Response) {
	response, err = s.modifier.ModifyErrorResponse(err, request, response)
	if err != nil {
		response.Body = []byte(ModifierPluginError.Error())
		response.StatusCode = 500
	}

	s.eventMetric(gatekeeper.RequestErrorEvent)
	s.writeResponse(rw, response)
}

// write a *gatekeeper.Response to an http.ResponseWriter
func (s *server) writeResponse(rw http.ResponseWriter, response *gatekeeper.Response) {
	rw.WriteHeader(response.StatusCode)

	for header, values := range response.Header {
		for _, value := range values {
			rw.Header().Set(header, value)
		}
	}

	// TODO: add metrics around this error to see where it happens in
	// practice; adding robustness once error edges have shown
	written, err := rw.Write(response.Body)
	if err != nil {
		log.Println(ResponseWriteError, err)
	} else if written != len(response.Body) {
		log.Println(ResponseWriteError)
	}
}

func (s *server) eventMetric(event gatekeeper.Event) {
	s.metricWriter.EventMetric(&gatekeeper.EventMetric{
		Event:     event,
		Timestamp: time.Now(),
	})
}
