package recaphttp

import (
	"fmt"
	"net/http"
	"regexp"
	"sync"
)

// Handler receives the standard HTTP values and the parsed legacy URI.
type Handler func(http.ResponseWriter, *http.Request, *URI)

type route struct {
	path    string
	method  string
	pattern *regexp.Regexp
	handler Handler
}

// Router dispatches full-path regular expressions by HTTP method.
type Router struct {
	mu     sync.RWMutex
	routes []route
}

// NewRouter returns an empty router.
func NewRouter() *Router {
	return &Router{}
}

// Add registers or replaces a route.
func (r *Router) Add(path string, methods []string, handler Handler) error {
	pattern, err := regexp.Compile("^(?:" + path + ")$")
	if err != nil {
		return fmt.Errorf("routeCompile[%q]: %w", path, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, method := range methods {
		isReplaced := false
		for index := range r.routes {
			if r.routes[index].path == path && r.routes[index].method == method {
				r.routes[index].handler = handler
				routes := r.routes[index]
				routes.pattern = pattern
				r.routes[index] = routes
				isReplaced = true
				break
			}
		}
		if !isReplaced {
			r.routes = append(r.routes, route{path: path, method: method, pattern: pattern, handler: handler})
		}
	}
	return nil
}

// Remove removes an exact path/method registration.
func (r *Router) Remove(path, method string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.routes {
		if r.routes[index].path == path && r.routes[index].method == method {
			r.routes = append(r.routes[:index], r.routes[index+1:]...)
			return
		}
	}
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	uri, err := ParseURI(request.RequestURI)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}

	r.mu.RLock()
	routes := append([]route(nil), r.routes...)
	r.mu.RUnlock()
	for _, candidate := range routes {
		if candidate.method == request.Method && candidate.pattern.MatchString(uri.Resource()) {
			candidate.handler(writer, request, uri)
			return
		}
	}
	http.NotFound(writer, request)
}
