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
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/bradleypeabody/gouuidv6"

	"github.com/go-redis/redis/v8"
	"github.com/kelseyhightower/envconfig"
)

type envInfo struct {
	StreamName       string `envconfig:"REDIS_STREAM_NAME"`
	RedisAddress     string `envconfig:"REDIS_ADDRESS"`
	RequestSizeLimit int    `envconfig:"REQUEST_SIZE_LIMIT"`
}

type requestData struct {
	ID      string //`json:"id"`
	Request string //`json:"request"`
}

type redisInterface interface {
	write(ctx context.Context, s envInfo, reqJSON []byte, id string) error
}

type myRedis struct {
	client redis.Cmdable
}

// request size limit in bytes
const bitsInMB = 1000000

var env envInfo
var rc redisInterface

func main() {
	// get env info for queue
	err := envconfig.Process("", &env)
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

// check for asynchronous header
func checkHeaderAndServe(w http.ResponseWriter, r *http.Request) {
	target := &url.URL{
		Scheme:   "http",
		Host:     r.Host,
		RawQuery: r.URL.RawQuery,
	}
	// check for Prefer: respond-async header
	if r.Header.Get("Prefer") == "respond-async" {
		// if body exists,
		if r.Body != nil {
			body, err := ioutil.ReadAll(io.LimitReader(r.Body, int64(env.RequestSizeLimit+1))) //check for length of body up to limit
			if err != nil {
				fmt.Println("Error reading body: ", err)
			}
			if len(body) > env.RequestSizeLimit {
				w.WriteHeader(500)
				fmt.Fprint(w, "body size exceeds limit of ", float64(env.RequestSizeLimit)/bitsInMB, " MB")
				return
			}
		}
		// write the request into b
		var buff = &bytes.Buffer{}
		if err := r.Write(buff); err != nil {
			fmt.Println("Error writing request ", r)
			// return err
		}
		// translate to string then json with id.
		reqString := buff.String()
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
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
		// TODO: do we need to close any connections or does writing the header handle this?
	}

	// Not async, proxy the request
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ServeHTTP(w, r)
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
