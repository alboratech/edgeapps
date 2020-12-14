// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2020 Intel Corporation

package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"github.com/pkg/errors"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

// Connectivity constants
const (
	EAAServerName = "eaa.openness"
	EAAServerPort = "443"
	EAACommonName = "eaa.openness"
	CertPath      = "./certs/cert.pem"
	RootCAPath    = "./certs/root.pem"
	KeyPath       = "./certs/key.pem"
)

var myURN URN

// VasConfig describes VAS JSON config file
type VasConfig struct {
	Acceleration string   `json:"Acceleration"`
	Framework    string   `json:"Framework"`
	Pipelines    []string `json:"Pipelines"`
}

// createEncryptedClient creates tls client with certs prorvided in
// CertPath, KeyPath
func createEncryptedClient() (*http.Client, error) {

	log.Println("Loading certificate and key")
	cert, err := tls.LoadX509KeyPair(CertPath, KeyPath)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to load client certificate")
	}

	certPool := x509.NewCertPool()
	caCert, err := ioutil.ReadFile(RootCAPath)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to load CA Cert")
	}
	ok := certPool.AppendCertsFromPEM(caCert)
	if !ok {
		return nil, errors.New("Failed to append cert")
	}

	// HTTPS client
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:      certPool,
				Certificates: []tls.Certificate{cert},
				ServerName:   EAACommonName,
			},
		},
		Timeout: 0,
	}
	log.Printf("%#v", client)

	return client, nil
}

func discoverServices(client *http.Client) (ServiceList, error) {
	var servList = ServiceList{}

	// Consumer discover services
	req, err := http.NewRequest("GET",
		"https://"+EAAServerName+":"+EAAServerPort+"/services", nil)
	if err != nil {
		log.Println("Service-discovery request creation failed:", err)
		return servList, err
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Service-discovery request failed:", err)
		return servList, err
	}
	reconnectTries := 0
	for resp.StatusCode == http.StatusServiceUnavailable && reconnectTries < 10 {
		reconnectTries++
		log.Println("EAA service is not currently available, trying again")
		time.Sleep(time.Duration(5) * time.Second)
		resp, err = client.Do(req)
		if err != nil {
			log.Println("Service-discovery request failed:", err)
			return servList, err
		}
	}
	if reconnectTries == 10 {
		log.Fatal("Number of connection retries to EAA Service Discovery exceeded, exiting")
	}

	// TODO check if service list is empty -> handle & exit program

	err = json.NewDecoder(resp.Body).Decode(&servList)
	if err != nil {
		log.Println("Service-list decode failed:", err)
		return servList, err
	}

	err = resp.Body.Close()
	if err != nil {
		return servList, err
	}

	return servList, nil
}

func main() {
	log.Println("Video-analytics-service Consumer Started")

	myURN = URN{
		ID:        "consumer",
		Namespace: "default",
	}

	// Authentication
	log.Println("Create Encrypted client")
	client, err := createEncryptedClient()
	if err != nil {
		log.Fatal(err)
		return
	}

	// Service Discovery
	log.Println("Service Discovery Started")
	servList, err := discoverServices(client)
	if err != nil {
		log.Fatal(err)
		return
	}

	sum := 0
	var vasInfo VasConfig
	for _, s := range servList.Services {
		sum++
		if sum > 100 {
			log.Fatal("abnormal services num")
			return
		}
		log.Println("Discovered service:")
		log.Println("    URN.ID:       ", s.URN.ID)
		log.Println("    URN.Namespace:", s.URN.Namespace)
		log.Println("    Description:  ", s.Description)
		log.Println("    EndpointURI:  ", s.EndpointURI)
		// Subscribe to all services related to my Namespace
		if myURN.Namespace == s.URN.Namespace {
			// Service Request to VA-Serving
			err := json.Unmarshal(s.Info, &vasInfo)
			if err != nil {
				log.Println(err)
			}
			postVAServingRequest(s.EndpointURI, vasInfo.Pipelines[0])
		} else {
			log.Println("Namespace mismatch, myURN namespace:", myURN.Namespace)
		}
	}
}
