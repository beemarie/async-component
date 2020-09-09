package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"time"

	"github.com/bradleypeabody/gouuidv6"

	"github.com/go-redis/redis/v8"
	"github.com/kelseyhightower/envconfig"
)

type EnvInfo struct {
	StreamName   string `envconfig:"REDIS_STREAM_NAME"`
	RedisAddress string `envconfig:"REDIS_ADDRESS"`
}

type RequestData struct {
	ID      string //`json:"id"`
	Request string //`json:"request"`
}

type RedisInterface interface {
	write(ctx context.Context, s EnvInfo, reqJSON []byte, id string) error
}

type MyRedis struct {
	client redis.Cmdable
}

// request size limit in bytes
const requestSizeLimit = 6000000
const bitsInMB = 1000000

var env EnvInfo
var rc RedisInterface

func main() {
	// get env info for queue
	err := envconfig.Process("", &env) // BMV TODO: how can we process just a subset of env?
	if err != nil {
		log.Fatal(err.Error())
	}

	// set up redis client
	opts := &redis.UniversalOptions{
		Addrs: []string{env.RedisAddress},
	}
	theclient := redis.NewUniversalClient(opts)
	rc = &MyRedis{
		client: theclient,
	}

	// Start an HTTP Server
	http.HandleFunc("/", checkHeaderAndServe)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func checkHeaderAndServe(w http.ResponseWriter, r *http.Request) {
	var isAsync bool
	target := &url.URL{
		Scheme:   "http",
		Host:     r.Host,
		Path:     r.URL.Path,
		RawQuery: r.URL.RawQuery,
	}
	// check for Prefer: respond-async header
	asyncHeader := r.Header.Get("Prefer")
	if asyncHeader == "respond-async" {
		isAsync = true
	}
	if !isAsync {
		proxy := httputil.NewSingleHostReverseProxy(target)
		r.Host = target.Host
		proxy.ServeHTTP(w, r)
	} else {
		// check for content-length if body exists
		if r.Body != nil {
			contentLength := r.Header.Get("Content-Length")
			if contentLength != "" {
				contentLength, err := strconv.Atoi(contentLength)
				if err != nil {
					fmt.Println("error converting contentLength to integer", err)
					// return err
				}
				if contentLength > requestSizeLimit {
					w.WriteHeader(500)
					fmt.Fprint(w, "Content-Length exceeds limit of ", float64(requestSizeLimit)/bitsInMB, " MB")
					return
				}
			} else { //if content length is empty, but body exists
				w.WriteHeader(411)
				fmt.Fprint(w, "Content-Length required with body")
			}
		}
		// write the request into b
		var b = &bytes.Buffer{}
		if err := r.Write(b); err != nil {
			fmt.Println("ERROR WRITING REQUEST")
			// return err
		}
		// translate to string then json with id.
		reqString := b.String()
		id := gouuidv6.NewFromTime(time.Now()).String()
		reqData := RequestData{
			ID:      id,
			Request: reqString,
		}
		reqJSON, err := json.Marshal(reqData)
		if err != nil {
			w.WriteHeader(500)
			fmt.Fprint(w, "Failed to marshal request: ", err)
			return
		}

		if sourceErr := rc.write(r.Context(), env, reqJSON, reqData.ID); sourceErr != nil {
			w.WriteHeader(500)
		} else {
			w.WriteHeader(http.StatusAccepted)
		}
		// BMV TODO: do we need to close any connections or does writing the header handle this?

	}
}

func (mr *MyRedis) write(ctx context.Context, s EnvInfo, reqJSON []byte, id string) (err error) {
	strCMD := mr.client.XAdd(ctx, &redis.XAddArgs{
		Stream: s.StreamName,
		Values: map[string]interface{}{
			"data": reqJSON,
		},
	})
	if strCMD.Err() != nil {
		log.Printf("Failed to publish %q %v", id, strCMD.Err())
		return strCMD.Err()
	}
	return
}