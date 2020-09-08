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

// request size limit in bytes
const requestSizeLimit = 6000000
const bitsInMB = 1000000

func main() {
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

		// get env info for queue
		var s EnvInfo
		err = envconfig.Process("", &s) // BMV TODO: how can we process just a subset of env, providing "kafka" maybe?
		if err != nil {
			log.Fatal(err.Error())
		}
		if sourceErr := writeToRedis(r.Context(), s, reqJSON, reqData.ID); sourceErr != nil {
			w.WriteHeader(500)
		} else {
			w.WriteHeader(http.StatusAccepted)
		}
		// BMV TODO: do we need to close any connections or does writing the header handle this?

	}
}

func writeToRedis(ctx context.Context, s EnvInfo, reqJSON []byte, id string) (err error) {
	opts := &redis.UniversalOptions{
		Addrs: []string{s.RedisAddress},
	}
	client := redis.NewUniversalClient(opts)
	strCMD := client.XAdd(ctx, &redis.XAddArgs{
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
