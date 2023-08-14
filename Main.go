/*
Copyright 2016 The Kubernetes Authors.

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

// Package butcherctl kills(scale to zero) dormant kubernetes services.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	"k8s.io/apimachinery/pkg/types"
	"log"
	//"log"
	"net/http"
	"strings"
	//"errors"

	//"k8s.io/apimachinery/pkg/types"
	//"k8s.io/apimachinery/pkg/util/strategicpatch"

	//"github.com/patrickmn/go-cache"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"strconv"
	"time"

	//"github.com/dgraph-io/ristretto"
	"k8s.io/client-go/kubernetes"
	//"k8s.io/api/core/v1"
	//"k8s.io/client-go/rest"
	//
	// Uncomment to load all auth plugins
	// _ "k8s.io/client-go/plugin/pkg/client/auth"
	//
	// Or uncomment to load specific auth plugins
	// _ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/openstack"
)

const VERSION_NUMBER = "Version: V2.0.0"

const BUTCHER_ASCII_LOGO = `
████████╗██╗  ██╗███████╗    ██████╗ ██╗   ██╗████████╗ ██████╗██╗  ██╗███████╗██████╗  ██████╗████████╗██╗     
╚══██╔══╝██║  ██║██╔════╝    ██╔══██╗██║   ██║╚══██╔══╝██╔════╝██║  ██║██╔════╝██╔══██╗██╔════╝╚══██╔══╝██║     
   ██║   ███████║█████╗      ██████╔╝██║   ██║   ██║   ██║     ███████║█████╗  ██████╔╝██║        ██║   ██║     
   ██║   ██╔══██║██╔══╝      ██╔══██╗██║   ██║   ██║   ██║     ██╔══██║██╔══╝  ██╔══██╗██║        ██║   ██║     
   ██║   ██║  ██║███████╗    ██████╔╝╚██████╔╝   ██║   ╚██████╗██║  ██║███████╗██║  ██║╚██████╗   ██║   ███████╗
   ╚═╝   ╚═╝  ╚═╝╚══════╝    ╚═════╝  ╚═════╝    ╚═╝    ╚═════╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝ ╚═════╝   ╚═╝   ╚══════╝
`

func printbutcher() {
	fmt.Fprint(os.Stderr, BUTCHER_ASCII_LOGO, VERSION_NUMBER)
}

// PodMetricsList : stores api metric  object
type PodMetricsList struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Metadata   struct {
		SelfLink string `json:"selfLink"`
	} `json:"metadata"`
	Items []struct {
		Metadata struct {
			Name              string    `json:"name"`
			Namespace         string    `json:"namespace"`
			SelfLink          string    `json:"selfLink"`
			CreationTimestamp time.Time `json:"creationTimestamp"`
		} `json:"metadata"`
		Timestamp  time.Time `json:"timestamp"`
		Window     string    `json:"window"`
		Containers []struct {
			Name  string `json:"name"`
			Usage struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

// Podstatus : stores kubernets pod object status
type Podstatus struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Status     struct {
		Phase             string    `json:"phase"`
		StartTime         time.Time `json:"startTime"`
		ContainerStatuses []struct {
			Name  string `json:"name"`
			State struct {
				Waiting struct {
					Reason  string `json:"reason"`
					Message string `json:"message"`
				} `json:"waiting"`
			} `json:"state"`
			LastState struct {
				Terminated struct {
					ExitCode    int       `json:"exitCode"`
					Reason      string    `json:"reason"`
					StartedAt   time.Time `json:"startedAt"`
					FinishedAt  time.Time `json:"finishedAt"`
					ContainerID string    `json:"containerID"`
				} `json:"terminated"`
			} `json:"lastState"`
			Ready        bool `json:"ready"`
			RestartCount int  `json:"restartCount"`
			Started      bool `json:"started"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

// patchUInt32Value specifies a patch operation for a uint32. - REMOVE
type patchUInt32Value struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value uint32 `json:"value"`
}

// recordState  Create a cache with a default expiration time of 5 minutes, and which
// purges expired items every 10 minutes
type recordState struct {
	pod, container           string
	strikes, podRestartCount int
}

// SlackRequestBody : stores slack request body
type SlackRequestBody struct {
	Text string `json:"text"`
}

// ctx connectToRedis connect to redis server
// context Leave blank for the default context in your kube config.
var (
	k8context  = ""
	ctx        = context.Background()
	webhookUrl = getEnv("SLACK_WEBHOOK", "https://hooks.slack.com/services/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")
)

// MakerCheckerId : stores maker checker id
const MakerCheckerId = "POD-"
const RedKeepIssue = "-------------------- !!! REDKEEP ISSUE: FAILED SAVING VIOLATION !!! --------------------"

// SendSlackNotification will post to an 'Incoming Webook' url setup in Slack Apps. It accepts
// some text and the slack channel is saved within Slack.
func SendSlackNotification(webhookUrl string, msg string) error {

	slackBody, _ := json.Marshal(SlackRequestBody{Text: msg})
	req, err := http.NewRequest(http.MethodPost, webhookUrl, bytes.NewBuffer(slackBody))
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body)
	if err != nil {
		return err
	}
	if buf.String() != "ok" {
		return errors.New("non-ok response returned from Slack")
	}
	return nil
}

// PanicAndRecover panic and recover method
func PanicAndRecover(message string) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Println(err)
		}
	}()
	panic(message)
}

// NukeApp - exit app and print error message if we cant recover from error
func NukeApp(err error) {
	_ = fmt.Errorf("%s", err)
	os.Exit(1)
}

// getEnv get key environment variable if exist otherwise return default Value
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return defaultValue
	}
	return value
}

// RedkeepClient Redis client constrcutor
func RedkeepClient() *redis.Client {
	//redisUri := getEnv("REDIS_URI", "redis-16369.c3.eu-west-1-2.ec2.cloud.redislabs.com:16369")
	//redisPassword := getEnv("REDIS_PASS", "UCU0Joj1RC70biz0s1HVTYhkXwgUn7Tj")
	//redisDB := getEnv("REDIS_DB", "0")
	// RED KEEP DEBUG CONFIG -- START
	//redisUri := getEnv("REDIS_URI", "localhost:55000")
	//redisPassword := getEnv("REDIS_PASS", "redispw")
	redisUri := getEnv("REDIS_URI", "localhost:6379")
	redisPassword := getEnv("REDIS_PASS", "")
	redisDB := getEnv("REDIS_DB", "0")
	// RED KEEP DEBUG CONFIG -- STOP

	redisDatabase, err := strconv.Atoi(redisDB)
	//redisDatabase, error := strconv.ParseInt()
	if err != nil {
		// handle error
		fmt.Println(err)
		os.Exit(2)
	}

	return redis.NewClient(&redis.Options{
		Addr:     redisUri,
		Password: redisPassword, // no password setz
		DB:       redisDatabase, // use default DB
	})
}

// RedKeepGet Records pod, container & strikes to redis and returns strike count
// Returns 0 if no record found
func (rs *recordState) RedKeepGet(pod string) (strikes int) {

	rdb := *RedkeepClient()
	//defer rdb.Close()

	val, err := rdb.Get(ctx, pod).Result()
	if err == redis.Nil {
		fmt.Println("-------------------- !!! REDKEEP: POD NOT FOUND: SAVING VIOLATION !!! --------------------", pod)
		rs.strikes = 0
		err := rdb.Set(ctx, pod, rs.strikes, 0).Err()
		if err != nil {
			PanicAndRecover(RedKeepIssue)
			fmt.Println("===========> | RedKeepGet | ERROR:", err)
			//NukeApp(err)
		}
	} else if err != nil {
		PanicAndRecover("-------------------- !!! REDKEEP ISSUE: CHECK YOUR REDIS CONNECTION !!! --------------------")
		NukeApp(err)
	}

	// if val is empty, return 0
	if val == "" {
		val = "0"
	}

	// Found pod in redis and returned current strike count. increment strike count
	fmt.Println("-------------------- !!! REDKEEP: POD FOUND !!! --------------------", pod, " | STRIKES: | ", val)

	currentStrikes, err := strconv.Atoi(val)

	//handle error
	if err != nil {
		PanicAndRecover(RedKeepIssue)
		fmt.Println("===========> | currentStrikes | ERROR:", err)
	}

	rs.strikes, err = strconv.Atoi(val)

	if err != nil {
		PanicAndRecover(RedKeepIssue)
		fmt.Println("===========> | rs.strikes | ERROR:", err)
		//NukeApp(err)
	}

	rs.strikes++

	rs.RedKeepPatch(pod, currentStrikes, rs.strikes)

	return rs.strikes
}

// RedKeepPatch  Patch/update pod metrics form redis
func (rs *recordState) RedKeepPatch(pod string, currnetstrikes, strikes int) {

	rdb := *RedkeepClient()

	fmt.Println("-------------------- !!! REDKEEP: FOUND :", pod, " | UPDATING strikes!  from: ", currnetstrikes, " to: ", strikes)
	err := rdb.Set(ctx, pod, strikes, 0).Err()
	if err != nil {
		PanicAndRecover("-------------------- !!! REDKEEP ISSUE: FAILD SAVING CHECK YORU REDIS CONNECTION !!! --------------------")
		//NukeApp(err)
	}
}

// RedKeepDelete Delete pod metrics form redis
func (rs *recordState) RedKeepDelete(pod, checkerId string) {
	// delete record form redis
	rdb := *RedkeepClient()

	cmd := rdb.Del(ctx, pod, checkerId).Err()
	if cmd != nil {
		//PanicAndRecover("-------------------- !!! REDKEEP ISSUE: FAILED DELETING KEY CHECK YOUR REDIS CONNECTION !!! --------------------")
		//fmterror := fmt.Errorf("REDIS ERROR : %s", err)
		//NukeApp(fmterror)
		fmt.Println("DELETED: ", cmd)
	}
}

// RedkeepMakerChecker returns, sets and counts number of violations recorded within an hour.
// RedkeepMakerChecker checks run every 5 minutes and are capped at 12
func (rs *recordState) RedkeepMakerChecker(podname string) (checks bool) {

	RedKeepChecks := rs.RedKeepGet(MakerCheckerId + podname)

	if RedKeepChecks >= 12 {
		fmt.Println("-------------------- !!! REDKEEP: MAXIMUM CHECKS REACHED !!! --------------------", RedKeepChecks)
		return true

	}
	fmt.Println("-------------------- !!! REDKEEP: CHECKS REMAINING !!! --------------------", RedKeepChecks)

	return false
}

// replicaSetName returns name of replica set
func replicaSetName(podname string) string {
	rawPodname := strings.Split(podname, "-")
	stripedrawPodname := rawPodname[:len(rawPodname)-2]
	return strings.Join(stripedrawPodname, "-")
}

// Record number of consecutive times a pod with cpu usage == 0 is found in-redis
func Record(podName, containerName, namespace string) {

	rs := recordState{}
	rs.pod = podName
	rs.container = containerName
	podShortName := replicaSetName(rs.pod)

	// check strikes limit = 10 and marker count limit = 12
	if rs.RedKeepGet(podName) >= 10 {
		if rs.RedkeepMakerChecker(podShortName) == true {
			fmt.Println("-------------------- !!! REDKEEP: KILL !!! --------------------", rs.container)
			ScaleDownPod(rs.pod, rs.container, namespace)
			rs.RedKeepDelete(rs.pod, MakerCheckerId+podShortName)
		}
	}
}

// ScaleDownPod takes a pod name and a number of strikes and scales down the pod to zero replica
func ScaleDownPod(pod, containerName, namespace string) {

	// Scale down deployment to zero
	fmt.Printf("********************************  | BUTCHER | Scaling down deployment for: %s. \n", pod)
	fmt.Println("===========================================================================================")

	c := isRunningInDockerContainer()

	podName := replicaSetName(pod)

	if err := scaleReplicationController(c, podName, namespace, 0); err != nil {
		fmt.Printf("********************************  | ALARM | Scaling down FAILED for: %s | %s. \n", pod, containerName)
		panic(err.Error())
	}

	err := SendSlackNotification(webhookUrl, "| *************  BUTCHERCTL ************* | Scaling down deployment for: "+pod)
	if err != nil {
		log.Fatal(err)
	}

}

// getK8Metrics returns a list of metrics from the k8s API and stores in PodMetricsList struct
func getK8Metrics(clientset *kubernetes.Clientset, pods *PodMetricsList) error {
	data, err := clientset.RESTClient().Get().AbsPath("apis/metrics.k8s.io/v1beta1/pods").DoRaw()
	if err != nil {
		fmt.Println("-------- API Error: getting metrics: ", err)
		return err
	}
	err = json.Unmarshal(data, &pods)
	return err
}

// getPodstatus returnns pod status from k8s API and stores it in a Podstatus struct
func getPodrestarts(clientset *kubernetes.Clientset, podstats *Podstatus, namespace, podname string) error {
	data, err := clientset.RESTClient().Get().AbsPath("api/v1/namespaces/" + namespace + "/pods/" + podname + "/status").DoRaw()
	if err != nil {
		//fmt.Println("-------------------- !!! K8S API ISSUE: FAILED GETTING POD STATUS !!! --------------------")
		fmt.Println("-------------------- !!! K8S API 404: POD NOT FOUND!!! --------------------", podname)
		//return err
		fmterror := fmt.Errorf("failed to get pod status: %s", err)
		//PanicAndRecover("RECOVERING: "+fmterror.Error())
		return fmterror
	}
	err = json.Unmarshal(data, &podstats)
	return err
}

// patchDeploymentReplicas returns the number of replicas for a deployment
// OPTION 2 PATTERN
func scaleReplicationController(clientSet *kubernetes.Clientset, replicasetName, namespace string, scale uint32) error {
	payload := []patchUInt32Value{{
		Op:    "replace",
		Path:  "/spec/replicas",
		Value: scale,
	}}
	payloadBytes, _ := json.Marshal(payload)
	data, err := clientSet.
		AppsV1().
		Deployments(namespace).
		Patch(replicasetName, types.JSONPatchType, payloadBytes)
	fmt.Println("****************** | REPLICAS: ", data.Status.Replicas)
	fmt.Println("****************** | AVAILABLE REPLICAS: ", data.Status.AvailableReplicas)
	fmt.Println("****************** | UNAVAILABLE REPLICAS: ", data.Status.UnavailableReplicas)
	return err
}

// OutClusterconfigv2 Gets cluster credentials from local kubeconfig
func OutClusterconfigv2() *kubernetes.Clientset {
	//  Get the local kube config.
	fmt.Printf("Connecting to Kubernetes Context %v\n", k8context)
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: k8context}).ClientConfig()
	if err != nil {
		panic(err.Error())
	}

	// Creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	return clientset
}

// InClusterconfig  Gets cluster credentials from incluster kubeconfig
func InClusterconfig() *kubernetes.Clientset {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	//fmt.Println("------> K8 INCLUSTER CLIENT: ",clientset)
	return clientset
}

// isRunningInDockerContainer checks if service is runnin in a docker container and returns incluster-kubeconfig insetad of local kubeconfig
func isRunningInDockerContainer() *kubernetes.Clientset {
	// docker creates a .dockerenv file at the root
	// of the directory tree inside the container.
	// if this file exists then the viewer is running
	// from inside a container so return true

	if _, err := os.Stat("/.dockerenv"); err == nil {
		fmt.Println("| RUNNING IN CONTAINER: USING IN-CLUSTER K8 CONFIG | ", time.Now(), " | ")
		return InClusterconfig()
	}
	fmt.Println("| RUNNING LOCAL: USING LOCAL K8 CONFIG | ", time.Now(), " | ")
	//return OutClusterconfig()
	return OutClusterconfigv2()
}

// getPodmetrics function gets pods with cpu usage == o no time window used its snapshot usage whan the fucntion is called.
func getPodmetrics(namespace string, chl chan string) {

	c := isRunningInDockerContainer()

	// get pod metrics and status from kubernetes api and return it
	var pods PodMetricsList
	var podRestartCount Podstatus

	if err := getK8Metrics(c, &pods); err != nil {
		fmt.Println("| INIT | ERROR: FAILED GETTING POD METRICS | ", time.Now(), " | ")
		panic(err.Error())
	}

	for {
		for _, pod := range pods.Items {
			if pod.Metadata.Namespace == namespace {
				//fmt.Println("| METRIC CHECK: START | ==== |",time.Now())
				for _, container := range pod.Containers {
					//fmt.Printf("POD: %s | NAMESPACE: %s. \n", pod.Metadata.Name, pod.Metadata.Namespace)
					switch container.Usage.CPU {
					case "0":
						//fmt.Printf("-------------------- !!! FATALITY !!! -------------------- CNAME: %s.\n", container.Name)

						// get pod restartCount from kubernetes api and return it
						//fmt.Println("--------------------| GET POD RESTARTS |" )
						if err := getPodrestarts(c, &podRestartCount, pod.Metadata.Namespace, pod.Metadata.Name); err != nil {
							//panic(err.Error())
							PanicAndRecover("RECOVERING: " + err.Error())
						}

						restartCount := 0
						for _, count := range podRestartCount.Status.ContainerStatuses {
							restartCount = count.RestartCount
						}
						//fmt.Printf("****************** POD: %s | NAMESPACE: %s | RESTART COUNT: %d. \n", pod.Metadata.Name, pod.Metadata.Namespace, restartCount)
						//fmt.Println("| METRIC CHECK: END | ==== |",time.Now())
						if restartCount >= 20 {
							fmt.Println("| COUNT FAIL | xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx | RES |", restartCount, " | POD | ", container.Name, " | ENV | ", namespace, " | ")
							Record(pod.Metadata.Name, container.Name, namespace)
						} //else{
						//	//fmt.Println("| ******************* | COUNT PASS | ************************************** | RES | ",restartCount," | POD | ",container.Name," | ENV | ", namespace," | ")
						//}

					default:
						//fmt.Printf("*PASS-->CPU: %s | MEM: %s | CNAME: %s.\n", container.Usage.CPU, container.Usage.Memory, container.Name)
						//fmt.Println("******************* ALL PASS **************************************")
						break
					}
				}
				//fmt.Println("| METRIC CHECK: STOP | ==== |",time.Now())
			}

		}

		//time.Sleep(30 * time.Minute)
		time.Sleep(30 * time.Second)
		chl <- "OK"
	}

}

// main starts the program
func main() {
	// BOOTUP LOGO
	printbutcher()

	// prints current date and time
	fmt.Println("| START | ", time.Now(), " | ")

	//kubernetes namespace to scan for pods
	namespaces := []string{"development", "testing"}

	// channel to send messages to main to sycnhronize goroutines
	ch := make(chan string)
	var data []string

	// limiting search cylces to 2600 to prevent from running out of memory
	for count := 0; count < 2600; count++ {
		for _, ns := range namespaces {
			go getPodmetrics(ns, ch)
			data = append(data, <-ch)
			break
		}
		time.Sleep(10 * time.Second)
		fmt.Println("| SCANS: |", len(data))
	}
}
