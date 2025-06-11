package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var sharedHTTPClient = &http.Client{Timeout: 10 * time.Second}

type DevCycleVariable struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Key         string `json:"key"`
	Type        string `json:"type,omitempty"`
}

type DevCycleVariation struct {
	Key       string         `json:"key"`
	Name      string         `json:"name"`
	Variables map[string]any `json:"variables"`
}

type SdkVisibility struct {
	Mobile bool `json:"mobile"`
	Client bool `json:"client"`
	Server bool `json:"server"`
}

type DevCycleNewFeaturePOSTBody struct {
	Name          string              `json:"name"`
	Key           string              `json:"key"`
	Description   string              `json:"description"`
	Variables     []DevCycleVariable  `json:"variables"`
	Variations    []DevCycleVariation `json:"variations"`
	SdkVisibility SdkVisibility       `json:"sdkVisibility"`
	Type          string              `json:"type"`
	Tags          []string            `json:"tags"`
}

// --- DevCycle API helpers ---

type devcycleAPI struct {
	baseURL string
	token   string
	client  *http.Client
}

// GetDevCycleOAuthToken requests an OAuth token from DevCycle using client credentials.
func GetDevCycleOAuthToken(clientID, clientSecret string) (string, error) {
	url := "https://auth.devcycle.com/oauth/token"

	payload := map[string]string{
		"grant_type":    "client_credentials",
		"client_id":     clientID,
		"client_secret": clientSecret,
		"audience":      "https://api.devcycle.com/",
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status: %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.AccessToken, nil
}

func newDevCycleAPI() *devcycleAPI {
	var token string
	if os.Getenv("DEVCYCLE_API_TOKEN") == "" {
		fmt.Println("DEVCYCLE_API_TOKEN environment variable is not set")
		if os.Getenv("DEVCYCLE_CLIENT_ID") != "" && os.Getenv("DEVCYCLE_CLIENT_SECRET") != "" {
			token, _ = GetDevCycleOAuthToken(os.Getenv("DEVCYCLE_CLIENT_ID"), os.Getenv("DEVCYCLE_CLIENT_SECRET"))
		}
	} else {
		token = os.Getenv("DEVCYCLE_API_TOKEN")
	}

	return &devcycleAPI{
		baseURL: "https://api.devcycle.com/v1",
		token:   token,
		client:  sharedHTTPClient,
	}
}

func (api *devcycleAPI) doRequest(method, url string, body any) (*http.Response, error) {
	var buf io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewBuffer(jsonData)
	}
	req, err := http.NewRequest(method, url, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+api.token)
	return api.client.Do(req)
}

func (api *devcycleAPI) getExistingCustomProperties(dvcProject string) ([]string, error) {
	url := fmt.Sprintf("%s/projects/%s/customProperties", api.baseURL, dvcProject)
	resp, err := api.doRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned error %d: %s", resp.StatusCode, string(body))
	}
	var response []struct {
		Key         string `json:"key"`
		PropertyKey string `json:"propertyKey"`
		Name        string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}
	var result []string
	for _, prop := range response {
		result = append(result, prop.PropertyKey)
	}
	return result, nil
}

func (api *devcycleAPI) createCustomProperty(dvcProject, key, dataType string) error {
	dvcType := "String"
	switch dataType {
	case "Boolean":
		dvcType = "Boolean"
	case "Number":
		dvcType = "Number"
	case "JSON":
		dvcType = "JSON"
	}
	payload := map[string]interface{}{
		"name":        key,
		"propertyKey": key,
		"key":         strings.ToLower(key),
		"type":        dvcType,
	}
	url := fmt.Sprintf("%s/projects/%s/customProperties", api.baseURL, dvcProject)
	resp, err := api.doRequest("POST", url, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API returned error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// --- Feature creation ---

func (api *devcycleAPI) createDevCycleFeature(dvcProject string, tlFeature TLImportRecord) error {
	featureKey := toKey(tlFeature.FeatureName)

	var variables []DevCycleVariable
	var dedupeVariables = make(map[string]string)
	var variations []DevCycleVariation

	variationDistributionPct := make(map[string]float64, len(tlFeature.Variations))

	for _, tlVariation := range tlFeature.Variations {
		variationValues := make(map[string]any, len(tlVariation.Variables))
		for _, tlVariable := range tlVariation.Variables {
			variationValues[toKey(tlVariable.Name)] = tlVariable.Value
			if _, exists := dedupeVariables[tlVariable.Name]; !exists {
				dedupeVariables[tlVariable.Name] = tlVariable.Type
				variables = append(variables, DevCycleVariable{
					Name:        tlVariable.Name,
					Key:         toKey(tlVariable.Name),
					Type:        convertTaplyticsVarTypeToDevCycle(tlVariable.Type),
					Description: fmt.Sprintf("Imported from Taplytics: %s", tlVariable.Name),
				})
			}
		}
		variations = append(variations, DevCycleVariation{
			Key:       toKey(tlVariation.Name),
			Name:      tlVariation.Name,
			Variables: variationValues,
		})
		variationDistributionPct[toKey(tlVariation.Name)] = tlVariation.Distribution
	}

	if len(variables) == 0 {
		fmt.Println("No variables to import for feature:", tlFeature.FeatureName)
		return nil
	}

	featurePayload := DevCycleNewFeaturePOSTBody{
		Name:          tlFeature.FeatureName,
		Key:           featureKey,
		Description:   fmt.Sprintf("Imported from Taplytics: %s", tlFeature.FeatureName),
		Variables:     variables,
		Variations:    variations,
		SdkVisibility: SdkVisibility{Mobile: true, Client: true, Server: true},
		Type:          "release",
		Tags:          tlFeature.Tags,
	}

	resp, err := api.doRequest("POST", fmt.Sprintf("%s/projects/%s/features", api.baseURL, dvcProject), featurePayload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Ignore duplicates
	if resp.StatusCode == http.StatusConflict {
		fmt.Println("Feature already exists, skipping creation:", tlFeature.FeatureName)
		return nil
	}

	if resp.StatusCode >= http.StatusInternalServerError {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("API returned 5xx error %d: %s", resp.StatusCode, string(body))
		time.Sleep(time.Second * 3)
		fmt.Println("Retrying feature creation...")
		return api.createDevCycleFeature(dvcProject, tlFeature)
	}

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API returned error %d: %s", resp.StatusCode, string(body))
	}

	var createResponse struct {
		ID         string `json:"_id"`
		Variations []struct {
			ID  string `json:"_id"`
			Key string `json:"key"`
		} `json:"variations"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&createResponse); err != nil {
		return fmt.Errorf("failed to parse feature creation response: %w", err)
	}

	variationIdMap := make(map[string]string)
	for _, variation := range createResponse.Variations {
		variationIdMap[variation.Key] = variation.ID
	}

	if len(tlFeature.Audience.Filters.Filters) > 0 {
		for _, env := range []string{"development", "staging", "production"} {
			if err := api.createTargetingRule(dvcProject, featureKey, env, tlFeature); err != nil {
				return fmt.Errorf("failed to create targeting rules: %w", err)
			}
		}
	}
	return nil
}

// --- Feature configuration (targeting rule) ---

func (api *devcycleAPI) createTargetingRule(dvcProject, featureKey, environmentKey string, tlFeature TLImportRecord) error {
	distribRecord := make([]map[string]interface{}, 0, len(tlFeature.Distribution))
	for _, dist := range tlFeature.Distribution {
		distribRecord = append(distribRecord, dist.ToAPIDistribution())
	}

	for _, filter := range tlFeature.Audience.Filters.Filters {
		switch filter.SubType {
		case "platformVersion":
			fallthrough
		case "appVersion":
			for i, v := range filter.Values {
				if str, ok := v.(string); ok {
					if len(strings.Split(str, ".")) == 2 {
						str += ".0" // Ensure it has a patch version
					}
					filter.Values[i] = str
				}
			}
		}
	}

	targetingRulePayload := map[string]interface{}{
		"audience":     tlFeature.Audience,
		"distribution": distribRecord,
	}

	configPayload := map[string]interface{}{
		"targets": []interface{}{targetingRulePayload},
		"status":  "active",
	}
	url := fmt.Sprintf("%s/projects/%s/features/%s/configurations?environment=%s", api.baseURL, dvcProject, featureKey, environmentKey)
	resp, err := api.doRequest("PATCH", url, configPayload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API returned error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (api *devcycleAPI) importFeaturesToDevCycle(dvcProject string, mergedFeatures map[string]TLImportRecord) error {
	// First, ensure all required custom data properties exist
	customDataProps := make(map[string]string)
	for _, feature := range mergedFeatures {
		for _, filter := range feature.Audience.Filters.Filters {
			if filter.SubType == "customData" {
				customDataProps[filter.DataKey] = filter.DataKeyType
			}
		}
	}

	if err := api.checkAndCreateCustomProperties(dvcProject, customDataProps); err != nil {
		return fmt.Errorf("failed to set up custom properties: %w", err)
	}

	// Import each feature
	for _, feature := range mergedFeatures {
		if err := api.createDevCycleFeature(dvcProject, feature); err != nil {
			return fmt.Errorf("failed to import feature %s: %w", feature.FeatureName, err)
		}
		fmt.Printf("Imported feature: %s\n", feature.FeatureName)
	}

	return nil
}

func (api *devcycleAPI) checkAndCreateCustomProperties(dvcProject string, customData map[string]string) error {
	existingProps, err := api.getExistingCustomProperties(dvcProject)
	if err != nil {
		return fmt.Errorf("failed to get existing custom properties: %w", err)
	}
	for _, prop := range existingProps {
		if _, exists := customData[prop]; exists {
			fmt.Println("Found existing custom property - skipping:", prop)
			delete(customData, prop) // Remove from customData if it already exists
		}
	}
	for key, dataType := range customData {
		if key == "" {
			continue
		}
		if err := api.createCustomProperty(dvcProject, key, dataType); err != nil {
			return fmt.Errorf("failed to create custom property %s: %w", key, err)
		}
		fmt.Printf("Created custom property: %s (%s)\n", key, dataType)
	}
	return nil
}
