/*
Copyright 2020 The Knative Authors
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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

type envInfo struct {
	StreamName   string `envconfig:"REDIS_STREAM_NAME"`
	RedisAddress string `envconfig:"REDIS_ADDRESS"`
}

type requestData struct {
	ID      string //`json:"id"`
	Request string //`json:"request"`
}

// wrapper with interface for testing redis
type redisInterface interface {
	write(ctx context.Context, s envInfo, reqJSON []byte, id string) error
}

type myRedis struct {
	client redis.Cmdable
}

// request size limit in bytes
const requestSizeLimit = 6000000
const bitsInMB = 1000000

var env envInfo
var rc redisInterface

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
	rc = &myRedis{
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
		if r.Body != http.NoBody {
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
				fmt.Println("r", r)
				fmt.Println("r body", r.Body)
				w.WriteHeader(411)
				fmt.Fprint(w, "Content-Length required with body")
				return
			}
		}
		// serialize the request
		// write the request into buffer
		var buffer = &bytes.Buffer{}
		if err := r.Write(buffer); err != nil {
			fmt.Println("Error writing request ", r)
			return
		}
		// translate to string then json with id.
		reqString := buffer.String()
		id := gouuidv6.NewFromTime(time.Now()).String()
		reqData := requestData{
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
		// TODO: do we need to close any connections or does writing the header handle this?
	}
}

func (mr *myRedis) write(ctx context.Context, s envInfo, reqJSON []byte, id string) (err error) {
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
