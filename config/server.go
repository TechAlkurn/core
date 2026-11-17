package config

import (
	"fmt"
	"net/http"
	"sync"
)

type serverList struct {
	Ports []string
}

func (srv *serverList) Populate() {
	srv.Ports = []string{"8001", "8002", "8003", "8004", "8005"}
}

func (srv *serverList) Pop() string {
	port := srv.Ports[0]
	srv.Ports = srv.Ports[1:]
	return port
}

func (srv *serverList) makeServers(wg *sync.WaitGroup) {
	defer wg.Done()
	port := srv.Pop()
	r := http.NewServeMux()
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "server %s!", port)
	})
	server := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}
	// Log the start of the server and the port it's running on.

	// Log.Infof("Hello from frontend %s server!", port)
	// Start the server. If it fails to start, log the error and return it.
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		Log.Fatalf("Failed to serve: %v", err)
	}
}

func InitServers() {
	var srv serverList
	srv.Populate()

	// Wait Group
	var wg sync.WaitGroup
	wg.Add(len(srv.Ports))
	defer wg.Wait()

	// Log.Info(" Init Servers! ")
	for range srv.Ports {
		go srv.makeServers(&wg)
	}
}
