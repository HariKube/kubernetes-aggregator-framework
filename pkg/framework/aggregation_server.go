package framework

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type watchEvent struct {
	Type   watch.EventType `json:"type"`
	Object any             `json:"object"`
}

type ServerConfig struct {
	KubeClient        client.Client
	DynamicKubeClient *dynamic.DynamicClient
	Port              string
	CertFile          string
	KeyFile           string
	Group             string
	Version           string
	APIKinds          []APIKind
}

type APIKind struct {
	ApiResource     metav1.APIResource
	RawEndpoints    map[string]http.HandlerFunc
	Resources       []Resource
	CustomResources []CustomResource
}
type Resource struct {
	CreateNew     ResourceCreateNew
	CreateNewList ResourceCreateNewList
	ListCallback  ResourceListCallback
	WatchCallback ResourceWatchCallback
}

type ResourceCreateNew func() (schema.GroupVersionResource, client.Object)

type ResourceCreateNewList func() (schema.GroupVersionResource, client.ObjectList)

type ResourceListCallback func(context.Context, string, string, client.ObjectList) (any, error)

type ResourceWatchCallback func(context.Context, string, string, *unstructured.Unstructured) (any, error)

type CustomResource struct {
	CreateHandler  CustomResourceHandlerFunc
	GetHandler     CustomResourceHandlerFunc
	ListHandler    CustomResourceHandlerFunc
	ReplaceHandler CustomResourceHandlerFunc
	DeleteHandler  CustomResourceHandlerFunc
	WatchHandler   CustomResourceHandlerFunc
}

type CustomResourceHandlerFunc func(string, string, http.ResponseWriter, *http.Request)

type Server struct {
	KubeClient         client.Client
	DynamicKubeCluient *dynamic.DynamicClient

	port     string
	certFile string
	keyFile  string
	mux      *http.ServeMux
}

func NewServer(config ServerConfig) *Server {
	srv := Server{
		KubeClient:         config.KubeClient,
		DynamicKubeCluient: config.DynamicKubeClient,
		port:               config.Port,
		certFile:           config.CertFile,
		keyFile:            config.KeyFile,
		mux:                http.NewServeMux(),
	}

	srv.mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	srv.mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})

	srv.mux.HandleFunc("/apis", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(&metav1.APIGroupList{
			Groups: []metav1.APIGroup{
				{
					Name: config.Group,
					Versions: []metav1.GroupVersionForDiscovery{
						{
							GroupVersion: config.Group + "/" + config.Version,
							Version:      config.Version,
						},
					},
					PreferredVersion: metav1.GroupVersionForDiscovery{
						GroupVersion: config.Group + "/" + config.Version,
						Version:      config.Version,
					},
				},
			},
		})
	})

	apiResources := []metav1.APIResource{}
	existingApiResources := map[string]bool{}
	resourceHandlersNamespaced := map[string]func(string, string, http.ResponseWriter, *http.Request){}
	for i := range config.APIKinds {
		ak := config.APIKinds[i]

		if _, ok := existingApiResources[ak.ApiResource.Name]; ok {
			panic("APIResource must be unique: " + ak.ApiResource.Name)
		}
		existingApiResources[ak.ApiResource.Name] = true

		apiResources = append(apiResources, ak.ApiResource)

		for ep, fn := range ak.RawEndpoints {
			srv.mux.HandleFunc("/apis/"+config.Group+"/"+config.Version+"/"+ak.ApiResource.Name+ep, fn)
		}

		for ii := range ak.Resources {
			res := ak.Resources[ii]

			srv.mux.HandleFunc("/apis/"+config.Group+"/"+config.Version+"/"+ak.ApiResource.Name, func(w http.ResponseWriter, r *http.Request) {
				srv.handleResourceFunc(
					res.CreateNew,
					res.CreateNewList,
					res.ListCallback,
					res.WatchCallback,
					"",
					"",
					w,
					r,
				)
			})

			if !ak.ApiResource.Namespaced {
				srv.mux.HandleFunc("/apis/"+config.Group+"/"+config.Version+"/"+ak.ApiResource.Name+"/", func(w http.ResponseWriter, r *http.Request) {
					parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/apis/"+config.Group+"/"+config.Version+"/"+ak.ApiResource.Name+"/"), "/")
					if parts[0] == "" {
						http.NotFound(w, r)
						return
					}

					srv.handleResourceFunc(
						res.CreateNew,
						res.CreateNewList,
						res.ListCallback,
						res.WatchCallback,
						"",
						parts[0],
						w,
						r,
					)
				})
			} else {
				resourceHandlersNamespaced[ak.ApiResource.Name] = func(namespace string, name string, w http.ResponseWriter, r *http.Request) {
					srv.handleResourceFunc(
						res.CreateNew,
						res.CreateNewList,
						res.ListCallback,
						res.WatchCallback,
						namespace,
						name,
						w,
						r,
					)
				}
			}
		}

		for ii := range ak.CustomResources {
			res := ak.CustomResources[ii]

			srv.mux.HandleFunc("/apis/"+config.Group+"/"+config.Version+"/"+ak.ApiResource.Name, func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					if strings.EqualFold(r.URL.Query().Get("watch"), "true") && res.WatchHandler != nil {
						res.WatchHandler("", "", w, r)
					} else if res.ListHandler != nil {
						res.ListHandler("", "", w, r)
					}
				default:
					http.NotFound(w, r)
				}
			})

			if !ak.ApiResource.Namespaced {
				srv.mux.HandleFunc("/apis/"+config.Group+"/"+config.Version+"/"+ak.ApiResource.Name+"/", func(w http.ResponseWriter, r *http.Request) {
					parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/apis/"+config.Group+"/"+config.Version+"/"+ak.ApiResource.Name+"/"), "/")
					if parts[0] == "" {
						http.NotFound(w, r)
						return
					}

					switch r.Method {
					case http.MethodGet:
						if strings.EqualFold(r.URL.Query().Get("watch"), "true") && res.WatchHandler != nil {
							res.WatchHandler("", parts[0], w, r)
						} else if res.ListHandler != nil && parts[0] == "" {
							res.ListHandler("", parts[0], w, r)
						} else if res.GetHandler != nil {
							res.GetHandler("", parts[0], w, r)
						}
					case http.MethodPut:
						if res.ReplaceHandler != nil {
							res.ReplaceHandler("", parts[0], w, r)
						}
					case http.MethodDelete:
						if res.DeleteHandler != nil {
							res.DeleteHandler("", parts[0], w, r)
						}
					case http.MethodPost:
						if res.CreateHandler != nil {
							res.CreateHandler("", "", w, r)
						}
					default:
						http.NotFound(w, r)
					}
				})
			} else {
				resourceHandlersNamespaced[ak.ApiResource.Name] = func(namespace string, name string, w http.ResponseWriter, r *http.Request) {
					switch r.Method {
					case http.MethodGet:
						if strings.EqualFold(r.URL.Query().Get("watch"), "true") && res.WatchHandler != nil {
							res.WatchHandler(namespace, name, w, r)
						} else if res.ListHandler != nil && name == "" {
							res.ListHandler(namespace, name, w, r)
						} else if res.GetHandler != nil {
							res.GetHandler(namespace, name, w, r)
						}
					case http.MethodPut:
						if res.ReplaceHandler != nil {
							res.ReplaceHandler(namespace, name, w, r)
						}
					case http.MethodDelete:
						if res.DeleteHandler != nil {
							res.DeleteHandler(namespace, name, w, r)
						}
					case http.MethodPost:
						if res.CreateHandler != nil {
							res.CreateHandler("", "", w, r)
						}
					default:
						http.NotFound(w, r)
					}
				}
			}
		}
	}

	srv.mux.HandleFunc("/apis/"+config.Group+"/"+config.Version, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(&metav1.APIResourceList{
			GroupVersion: config.Group + "/" + config.Version,
			APIResources: apiResources,
		})
	})

	if len(resourceHandlersNamespaced) != 0 {
		srv.mux.HandleFunc("/apis/"+config.Group+"/"+config.Version+"/namespaces/", func(w http.ResponseWriter, r *http.Request) {
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/apis/"+config.Group+"/"+config.Version+"/namespaces/"), "/")
			if len(parts) < 2 {
				http.NotFound(w, r)
				return
			}

			handlerFunc, ok := resourceHandlersNamespaced[parts[1]]
			if !ok {
				http.NotFound(w, r)
				return
			}

			namespace := parts[0]
			name := ""
			if len(parts) == 3 {
				name = parts[2]
			}

			handlerFunc(namespace, name, w, r)
		})
	}

	return &srv
}

func (s *Server) Start(ctx context.Context) (err error) {
	srv := http.Server{
		Addr:      s.port,
		Handler:   s.mux,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}

	go func() {
		if listenErr := srv.ListenAndServeTLS(s.certFile, s.keyFile); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			err = listenErr
		}
	}()

	<-ctx.Done()

	if err = srv.Shutdown(context.Background()); err != nil {
		return err
	}

	return nil
}

func (s *Server) handleResourceFunc(
	createNew ResourceCreateNew,
	createNewList ResourceCreateNewList,
	listCallback ResourceListCallback,
	watchCallback ResourceWatchCallback,
	namespace,
	name string,
	w http.ResponseWriter,
	r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "only GET", http.StatusMethodNotAllowed)
		return
	}

	newGVR, _ := createNew()
	newKind, err := s.KubeClient.RESTMapper().KindFor(newGVR)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to find kind: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	newListGVR, emptyList := createNewList()
	newKindList, err := s.KubeClient.RESTMapper().KindFor(newListGVR)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to find list kind: %s", err.Error()), http.StatusInternalServerError)
		return
	}
	emptyList.GetObjectKind().SetGroupVersionKind(newKindList)

	if strings.EqualFold(r.URL.Query().Get("watch"), "true") {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported by server", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json;stream=watch; charset=utf-8")
		enc := json.NewEncoder(w)

		allowBookmarks := strings.EqualFold(r.URL.Query().Get("allowWatchBookmarks"), "true")

		rv := r.URL.Query().Get("resourceVersion")

		var rvm = metav1.ResourceVersionMatchNotOlderThan
		switch r.URL.Query().Get("resourceVersionMatch") {
		case string(metav1.ResourceVersionMatchExact):
			rvm = metav1.ResourceVersionMatchExact
		case string(metav1.ResourceVersionMatchNotOlderThan):
			rvm = metav1.ResourceVersionMatchNotOlderThan
		}

		sendInitial := rv == "" || rv == "0"
		if sip := r.URL.Query().Get("sendInitialEvents"); sip != "" {
			sendInitial = strings.EqualFold(sip, "true")
		}

		if sendInitial && rvm != metav1.ResourceVersionMatchNotOlderThan {
			http.Error(w, "sendInitialEvents requires resourceVersionMatch=NotOlderThan", http.StatusBadRequest)
			return
		}

		timeoutSec := int64(60)
		if rawTimeoutSec := r.URL.Query().Get("timeoutSeconds"); rawTimeoutSec != "" {
			if v, err := strconv.ParseInt(rawTimeoutSec, 10, 64); err == nil && v > 0 {
				timeoutSec = v
			}
		}

		fs := r.URL.Query().Get("fieldSelector")
		if name != "" {
			fs = "metadata.name=" + name
		}

		listOpts := metav1.ListOptions{
			ResourceVersion:      rv,
			ResourceVersionMatch: rvm,
			TimeoutSeconds:       &timeoutSec,
			SendInitialEvents:    &sendInitial,
			Watch:                true,
			AllowWatchBookmarks:  allowBookmarks,
			LabelSelector:        r.URL.Query().Get("labelSelector"),
			FieldSelector:        fs,
		}

		watcher, err := s.DynamicKubeCluient.Resource(newGVR).Namespace(namespace).Watch(r.Context(), listOpts)
		if err != nil {
			var se *apierrors.StatusError
			if errors.As(err, &se) && se.ErrStatus.Code == http.StatusGone {
				http.Error(w, err.Error(), http.StatusGone)
				return
			}

			http.Error(w, "failed to initialize watcher: "+err.Error(), http.StatusInternalServerError)
			return
		}

		for {
			select {
			case <-r.Context().Done():
				watcher.Stop()
				return
			case event, ok := <-watcher.ResultChan():
				if !ok {
					return
				}

				if event.Type == watch.Error {
					if status, ok := event.Object.(*metav1.Status); ok {
						http.Error(w, fmt.Sprintf("watch error: %s", status.Message), int(status.Code))
						return
					}

					http.Error(w, "watch error: unknown", http.StatusInternalServerError)
					return
				} else if event.Type == watch.Bookmark {
					if allowBookmarks {
						if err := enc.Encode(event); err != nil {
							http.Error(w, "failed to send bookmark: "+err.Error(), http.StatusInternalServerError)
							return
						}

						flusher.Flush()
					}

					continue
				} else if event.Object == nil {
					continue
				}

				event.Object.GetObjectKind().SetGroupVersionKind(newKind)

				unstructuredObj, ok := event.Object.(*unstructured.Unstructured)
				if !ok {
					http.Error(w, "failed to cast to unstructured", http.StatusInternalServerError)
					return
				}

				var res any = unstructuredObj
				if watchCallback != nil {
					res, err = watchCallback(r.Context(), namespace, name, unstructuredObj)
					if err != nil {
						http.Error(w, fmt.Sprintf("watch error: %s", err.Error()), http.StatusInternalServerError)
						return
					}
				}

				if err = enc.Encode(watchEvent{
					Type:   event.Type,
					Object: res,
				}); err != nil {
					http.Error(w, "failed to encode watch response: "+err.Error(), http.StatusInternalServerError)
					return
				}

				flusher.Flush()
			}
		}
	}

	_, items := createNewList()
	if name != "" {
		_, item := createNew()
		if err := s.KubeClient.Get(r.Context(), client.ObjectKey{
			Namespace: namespace, Name: name,
		}, item); err != nil {
			if client.IgnoreNotFound(err) != nil {
				http.Error(w, "failed to get task: "+err.Error(), http.StatusInternalServerError)
				return
			}

			http.NotFound(w, r)
			return
		}

		items.SetResourceVersion(item.GetResourceVersion())
		meta.SetList(items, []runtime.Object{item})
	} else {
		var limit int64 = 500
		if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
			if l, err := strconv.ParseInt(rawLimit, 10, 64); err == nil && l > 0 && l <= 1000 {
				limit = l
			}
		}

		listOpts := client.ListOptions{
			Namespace: namespace,
			Limit:     limit,
			Continue:  r.URL.Query().Get("continue"),
			Raw:       &metav1.ListOptions{},
		}

		if ls := r.URL.Query().Get("labelSelector"); ls != "" {
			selector, err := labels.Parse(ls)
			if err != nil {
				http.Error(w, "failed to parse labelSelector: "+err.Error(), http.StatusBadRequest)
				return
			}
			listOpts.LabelSelector = selector
		}

		if fs := r.URL.Query().Get("fieldSelector"); fs != "" {
			selector, err := fields.ParseSelector(fs)
			if err != nil {
				http.Error(w, "failed to parse fieldSelector: "+err.Error(), http.StatusBadRequest)
				return
			}
			listOpts.FieldSelector = selector
		}

		if rv := r.URL.Query().Get("resourceVersion"); rv != "" {
			listOpts.Raw.ResourceVersion = rv
		}

		listOpts.Raw.ResourceVersionMatch = metav1.ResourceVersionMatchNotOlderThan
		switch r.URL.Query().Get("resourceVersionMatch") {
		case string(metav1.ResourceVersionMatchExact):
			listOpts.Raw.ResourceVersionMatch = metav1.ResourceVersionMatchExact
		case string(metav1.ResourceVersionMatchNotOlderThan):
			listOpts.Raw.ResourceVersionMatch = metav1.ResourceVersionMatchNotOlderThan
		}

		if err := s.KubeClient.List(r.Context(), items, &listOpts); err != nil {
			var se *apierrors.StatusError
			if errors.As(err, &se) && se.ErrStatus.Code == http.StatusGone {
				http.Error(w, err.Error(), http.StatusGone)
				return
			}

			http.Error(w, "failed to list tasks: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	items.GetObjectKind().SetGroupVersionKind(newKindList)

	if meta.LenList(items) == 0 {
		if err := json.NewEncoder(w).Encode(items); err != nil {
			http.Error(w, "failed to encode response: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if err := meta.EachListItem(items, func(o runtime.Object) error {
		o.GetObjectKind().SetGroupVersionKind(newKind)
		return nil
	}); err != nil {
		http.Error(w, "failed to set group version kind: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var res any = items
	if listCallback != nil {
		var err error
		res, err = listCallback(r.Context(), namespace, name, items)
		if err != nil {
			http.Error(w, "failed to list tasks: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if name != "" {
		if itemList, ok := res.(runtime.Object); ok {
			if items, err := meta.ExtractList(itemList); err == nil {
				res = items[0]
			}
		}
	}

	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, "failed to encode response: "+err.Error(), http.StatusInternalServerError)
		return
	}
}
