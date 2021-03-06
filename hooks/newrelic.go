package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/blablacar/contactkey/utils"
	log "github.com/sirupsen/logrus"
)

type NewRelicClient struct {
	HttpClient        *http.Client
	Url               string
	ApiKey            string
	ApplicationFilter string
	ApplicationId     int
	Stop              bool
}

type NewRelicDeployment struct {
	Revision    string `json:"revision"`
	Changelog   string `json:"changelog"`
	Description string `json:"description"`
	User        string `json:"user"`
}

type NewRelicApplicationList struct {
	Applications []struct {
		Id   int    `json:"id"`
		Name string `json:"Name"`
	} `json:"applications"`
}

func (c NewRelicClient) Init() error {
	return nil
}

func (c NewRelicClient) PreDeployment(userName string, env string, service string, podVersion string) error {
	var filter bytes.Buffer
	filterTmpl, err := template.New("filter").Parse(c.ApplicationFilter)
	if err != nil {
		return err
	}

	if err := filterTmpl.Execute(&filter, struct{ env string }{env}); err != nil {
	}

	appId, err := c.findApplicationId(filter.String())
	if err != nil {
		return err
	}
	c.ApplicationId = appId

	description := fmt.Sprintf("Deploying %s %s on %s", service, podVersion, env)
	d := &NewRelicDeployment{
		Description: description,
		Revision:    podVersion,
		User:        userName,
	}
	return c.CreateDeployment(d)
}

func (c NewRelicClient) PostDeployment(userName string, env string, service string, podVersion string) error {
	return nil
}

func (c NewRelicClient) StopOnError() bool {
	return c.Stop
}

func NewNewRelicClient(cfg utils.NewRelicConfig, manifest utils.NewRelicManifest) (*NewRelicClient, error) {
	if cfg.Url == "" {
		return nil, errors.New("You need to define an url for newrelic in the config.")
	}

	if cfg.ApiKey == "" {
		return nil, errors.New("You need to define an apiKey for newrelic in the config.")
	}

	if manifest.ApplicationFilter == "" {
		return nil, errors.New("You need to define an applicationId for newrelic in the manifest.")
	}

	c := &NewRelicClient{
		HttpClient:        &http.Client{},
		Url:               cfg.Url,
		ApiKey:            cfg.ApiKey,
		Stop:              manifest.StopOnError,
		ApplicationFilter: manifest.ApplicationFilter,
	}

	return c, nil
}

func (c NewRelicClient) findApplicationId(nameFilter string) (int, error) {
	var applications NewRelicApplicationList

	filter := strings.NewReader(fmt.Sprintf("filter[name]=%s", nameFilter))
	request, err := c.NewRequest("GET", "v2/applications.json", filter)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := c.HttpClient.Do(request)
	if err != nil {
		return 0, err
	}

	defer response.Body.Close()
	bodyJson, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return 0, err
	}
	log.WithFields(log.Fields{
		"statusCode": response.StatusCode,
		"body":       string(bodyJson),
	}).Debug("NewRelic response")

	if response.StatusCode != http.StatusOK {
		return 0, errors.New("HTTP error from NewRelic")
	}

	err = json.Unmarshal(bodyJson, &applications)
	if err != nil {
		return 0, err
	}

	if len(applications.Applications) == 0 {
		return 0, fmt.Errorf("application %s not found", nameFilter)
	}

	return applications.Applications[0].Id, nil
}

func (c NewRelicClient) NewRequest(method string, route string, body io.Reader) (*http.Request, error) {
	url := fmt.Sprintf("%s/%s", c.Url, route)

	request, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	request.Header.Add("X-Api-Key", c.ApiKey)

	log.WithFields(log.Fields{
		"url":    request.URL,
		"method": request.Method,
	}).Debug("NewRelic request")
	return request, nil
}

type NewRelicDeploymentBody struct {
	Deployment *NewRelicDeployment `json:"deployment"`
}

// https://rpm.newrelic.com/api/explore/application_deployments/create
func (c NewRelicClient) CreateDeployment(d *NewRelicDeployment) error {
	body := &bytes.Buffer{}
	if err := json.NewEncoder(body).Encode(
		NewRelicDeploymentBody{Deployment: d},
	); err != nil {
		return err
	}

	request, err := c.NewRequest("POST", fmt.Sprintf("v2/applications/%d/deployments.json", c.ApplicationId), body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.HttpClient.Do(request)
	log.WithFields(log.Fields{
		"statusCode": response.StatusCode,
	}).Debug("NewRelic response")
	if err != nil {
		return err
	}

	if response.StatusCode != http.StatusCreated {
		return errors.New(fmt.Sprintf("NewRelic status code: %d", response.StatusCode))
	}

	return nil
}
