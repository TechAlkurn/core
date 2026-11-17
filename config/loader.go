package config

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gin-gonic/gin"
)

/*
var baseURLs = []string{
	"http://127.0.0.1:8001", // Frontend server 1
	"http://127.0.0.1:8002", // Frontend server 2
	"http://127.0.0.1:8003", // Frontend server 3
	"http://127.0.0.1:8004", // Frontend server 4
	"http://127.0.0.1:8005", // Frontend server 4
	// Add more frontend servers as needed
}

type LoadBalancer struct {
	ReverseProxy httputil.ReverseProxy
}

type EndPoints struct {
	List []*url.URL
}

func (ep *EndPoints) Shuffle() {
	temp := ep.List[0]
	ep.List = ep.List[1:]
	ep.List = append(ep.List, temp)
}

func MakeLoadBalancer() {
	var lb LoadBalancer
	var ep EndPoints

	router := http.NewServeMux()
	server := &http.Server{
		Addr:    "8000",
		Handler: router,
	}
	for _, r := range baseURLs {
		url, _ := url.Parse(r)
		ep.List = append(ep.List, url)
	}
	router.HandleFunc("/load", makeRequest(&lb, &ep))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		Log.Fatalf("Failed to serve: %v", err)
	}
}

func makeRequest(lb *LoadBalancer, ep *EndPoints) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		lb.ReverseProxy = *httputil.NewSingleHostReverseProxy(ep.List[0])
		ep.Shuffle()
		lb.ReverseProxy.ServeHTTP(w, r)
	}
}

func FrLoadBalancer() http.Handler {
	frURLs := []string{
		"http://127.0.0.1:8001", // Frontend server 1
		"http://127.0.0.1:8002", // Frontend server 2
		"http://127.0.0.1:8003", // Frontend server 3
		"http://127.0.0.1:8004", // Frontend server 4
		"http://127.0.0.1:8005", // Frontend server 5
		// Add more frontend servers as needed
	}
	var proxyServers []*httputil.ReverseProxy
	for _, frURL := range frURLs {
		target, err := url.Parse(frURL)
		if err != nil {
			Log.Fatalf("Failed to parse proxy URL: %v", err)
		}
		proxyServers = append(proxyServers, httputil.NewSingleHostReverseProxy(target))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simple round-robin load balancing
		proxy := proxyServers[rand.Intn(len(proxyServers))]
		proxy.ServeHTTP(w, r)
	})
} */

// LoadBalancer is a simple round-robin load balancer.
type loader struct {
	servers []string
	next    int
}

// NewLoadBalancer creates a new LoadBalancer with the given list of backend servers.
func NewLoader(servers []string) *loader {
	return &loader{
		servers: servers,
	}
}

func (lb *loader) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	port := lb.servers[lb.next]
	lb.next = (lb.next + 1) % len(lb.servers)
	proxy := httputil.NewSingleHostReverseProxy(&url.URL{
		Scheme: "http",
		Host:   "127.0.0.1:" + port,
	})
	proxy.ServeHTTP(w, r)
}

func RunServer(ctx context.Context, r *gin.Engine, port string) error {
	server := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}
	Log.Debugf("Starting Load Balancer Server on port %s", port)
	return server.ListenAndServe()
}
