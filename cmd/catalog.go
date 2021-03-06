//
// Copyright (c) 2018 Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package cmd

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/automationbroker/apb/pkg/config"

	"github.com/automationbroker/bundle-lib/clients"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type clusterServiceBrokerSpec struct {
	RelistRequests int
}

type relistResponse struct {
	Kind string
	Spec clusterServiceBrokerSpec
}

var brokerResourceName string

var catalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "Interact with OpenShift Service Catalog",
	Long:  `Force the OpenShift Service Catalog to relist its group of APB specs`,
}

var catalogRelistCmd = &cobra.Command{
	Use:   "relist",
	Short: "relist service catalog",
	Long:  `Force a relist of the OpenShift Service Catalog`,
	Run: func(cmd *cobra.Command, args []string) {
		relistCatalog(config.LoadedDefaults.BrokerResourceURL, config.LoadedDefaults.ClusterServiceBrokerName)
	},
}

func init() {
	rootCmd.AddCommand(catalogCmd)
	// Catalog Relist Flags
	catalogCmd.PersistentFlags().StringVarP(&brokerResourceName, "name", "n", "", "Name of Automation Broker resource")
	catalogCmd.AddCommand(catalogRelistCmd)
}

func relistCatalog(brokerResourceURL string, clusterServiceBrokerName string) {
	log.Debug("relistCatalog called")
	// Override setting from config file when cmd arg provided
	if brokerResourceName != "" {
		clusterServiceBrokerName = brokerResourceName
	}
	kube, err := clients.Kubernetes()
	if err != nil {
		log.Errorf("Failed to connect to cluster: %v", err)
		return
	}
	// Check for user with valid bearer token
	if kube.ClientConfig.BearerToken == "" {
		handleBearerTokenErr()
		return
	}
	// Get Cluster URL and form clusterservicebroker request
	host := kube.ClientConfig.Host
	brokerURL := fmt.Sprintf("%v%v%v", host, brokerResourceURL, clusterServiceBrokerName)

	req, err := http.NewRequest("GET", brokerURL, nil)
	if err != nil {
		log.Errorf("Failed to create relist request: %v", err)
		return
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %v", kube.ClientConfig.BearerToken))
	// Skip TLS for now
	cfg := &tls.Config{
		InsecureSkipVerify: true,
	}
	http.DefaultClient.Transport = &http.Transport{
		TLSClientConfig: cfg,
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Errorf("Failed to get relist response: %v", err)
	}
	defer resp.Body.Close()
	// Special case for 404 to tell user about --name flag
	if resp.StatusCode == 404 {
		log.Errorf("Failed to find clusterservicebroker resource [%v]. Try specifying name with --name flag.", brokerURL)
		return
	}
	if resp.StatusCode != 200 {
		respBody, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Errorf("Failed to read relist response body: %v", err)
			return
		}
		log.Errorf("Bad relist response. Expected status 200, got: %v\n%s", resp.StatusCode, respBody)
		if bytes.Contains(respBody, []byte("cannot get clusterservicebrokers")) {
			handleResourceInaccessibleErr("clusterservicebrokers", "", true)
		}
		return
	}
	// Read response and unmarshal to get relistRequest count
	jsonRelist, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Failed to read relist response body: %v", err)
		return
	}
	relistResp := relistResponse{}
	err = json.Unmarshal(jsonRelist, &relistResp)
	if err != nil {
		log.Errorf("Failed to unmarshal relist response: %v", err)
		return
	}
	// Increment relist requests and PATCH clusterservicebroker resource
	newRelistCount := relistResp.Spec.RelistRequests + 1
	var patchRequest = []byte(fmt.Sprintf("{\"spec\": {\"relistRequests\": %v}}", newRelistCount))
	req, err = http.NewRequest("PATCH", brokerURL, bytes.NewBuffer(patchRequest))
	if err != nil {
		log.Errorf("Failed to create patch relist request: %v", err)
		return
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %v", kube.ClientConfig.BearerToken))
	req.Header.Set("Content-Type", "application/strategic-merge-patch+json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		log.Errorf("Failed to send PATCH relist request: %v", err)
		return
	}
	if resp.StatusCode != 200 {
		log.Errorf("Error: Relist status code is not 200, got: %v", resp.Status)
		return
	}
	fmt.Printf("Successfully relisted OpenShift Service Catalog for [%v]\n", clusterServiceBrokerName)
	return
}
