package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

func createVariationValues(variables []DevCycleVariable, isEnabled bool) map[string]any {
	result := make(map[string]any)

	for _, variable := range variables {
		result[variable.Key] = getDefaultValue(variable.Type, isEnabled)
	}

	return result
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

func (api *devcycleAPI) getExistingCustomProperties(dvcProject string) (map[string]struct{}, error) {
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
		Key string `json:"key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}
	result := make(map[string]struct{})
	for _, prop := range response {
		result[prop.Key] = struct{}{}
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
	}
	payload := map[string]interface{}{
		"name":        key,
		"propertyKey": key,
		"key":         key,
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
	for _, tlVar := range tlFeature.Variables {
		variables = append(variables, DevCycleVariable{
			Name:        tlVar.Name,
			Key:         toKey(tlVar.Name),
			Type:        convertTaplyticsVarTypeToDevCycle(tlVar.Type),
			Description: fmt.Sprintf("Imported from Taplytics: %s", tlVar.Name),
		})
	}

	variations := []DevCycleVariation{
		{Key: "imported-on", Name: "On", Variables: createVariationValues(variables, false)},
		{Key: "imported-off", Name: "Off", Variables: createVariationValues(variables, true)},
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
	if err := json.NewDecoder(resp.Body).Decode(&createResponse); err != nil {
		return fmt.Errorf("failed to parse feature creation response: %w", err)
	}
	variationKeyToID := make(map[string]string)
	for _, v := range createResponse.Variations {
		variationKeyToID[v.Key] = v.ID
	}

	if len(tlFeature.Audience.Filters.Filters) > 0 {
		if variationID, ok := variationKeyToID["imported-on"]; ok {
			if err := api.createTargetingRule(dvcProject, featureKey, os.Getenv("DEVCYCLE_ENVIRONMENT_KEY"), tlFeature, variationID); err != nil {
				return fmt.Errorf("failed to create targeting rules: %w", err)
			}
		} else {
			return fmt.Errorf("could not find variation ID for 'imported-on'")
		}
	}
	return nil
}

// --- Feature configuration (targeting rule) ---

func (api *devcycleAPI) createTargetingRule(dvcProject, featureKey, environmentKey string, tlFeature TLImportRecord, variationID string) error {
	audience := map[string]interface{}{
		"name":    tlFeature.Audience.Name,
		"filters": convertTLFiltersToDevCycleTargeting(tlFeature.Audience.Filters),
	}
	target := map[string]interface{}{
		"name":     tlFeature.Audience.Name,
		"audience": audience,
		"distribution": []map[string]interface{}{{
			"_variation": variationID,
			"percentage": 1.0,
		}},
	}
	configPayload := map[string]interface{}{
		"targets": []interface{}{target},
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

// DevCycleFeatureConfig represents a feature configuration in DevCycle
type DevCycleFeatureConfig struct {
	Name           string                  `json:"name"`
	FeatureKey     string                  `json:"featureKey"`
	Enabled        bool                    `json:"enabled"`
	TargetingRules []DevCycleTargetingRule `json:"targetingRules,omitempty"`
	ServingRules   []interface{}           `json:"servingRules,omitempty"`
	VariationKey   string                  `json:"variationKey,omitempty"`
	Variables      map[string]interface{}  `json:"variables,omitempty"`
}

// DevCycleTargetingRule represents a targeting rule in DevCycle
type DevCycleTargetingRule struct {
	Name         string                 `json:"name"`
	Audience     map[string]interface{} `json:"audience,omitempty"`
	VariationKey string                 `json:"variationKey,omitempty"`
	Variables    map[string]interface{} `json:"variables,omitempty"`
}

// CreateFeatureConfiguration creates a feature configuration (targeting rule) in DevCycle
func CreateFeatureConfiguration(
	apiKey string,
	projectKey string,
	featureKey string,
	tlAudience TLAudience,
	variationKey string,
	variables map[string]interface{},
) error {
	// Build the feature configuration
	featureConfig := DevCycleFeatureConfig{
		Name:         fmt.Sprintf("%s Configuration", featureKey),
		FeatureKey:   featureKey,
		Enabled:      true,
		VariationKey: variationKey,
		Variables:    variables,
	}

	// If there's audience targeting, add a targeting rule
	if tlAudience.Name != "" {
		targetingRule := DevCycleTargetingRule{
			Name:         tlAudience.Name,
			Audience:     convertTLFiltersToDevCycleTargeting(tlAudience.Filters),
			VariationKey: variationKey,
			Variables:    variables,
		}
		featureConfig.TargetingRules = []DevCycleTargetingRule{targetingRule}
	}

	// Create the feature configuration via API
	url := fmt.Sprintf("https://api.devcycle.com/v1/projects/%s/features/%s/configurations", projectKey, featureKey)

	body, err := json.Marshal(featureConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal feature configuration: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("failed to create feature configuration: HTTP %d", resp.StatusCode)
	}

	return nil
}

// ConvertAndCreateFeatureConfig converts Taplytics record to DevCycle feature config
func ConvertAndCreateFeatureConfig(
	apiKey string,
	projectKey string,
	record TLImportRecord,
	featureKey string,
) error {
	// Default variation is true (enabled)
	variationKey := "true"

	// Build variables map from record.Variables
	variables := make(map[string]interface{})
	for _, v := range record.Variables {
		varKey := toKey(fmt.Sprintf("%s_%s", record.FeatureName, v.Name))
		// Default values based on type
		switch convertTaplyticsVarTypeToDevCycle(v.Type) {
		case "Boolean":
			variables[varKey] = true
		case "Number":
			variables[varKey] = 1
		case "String":
			variables[varKey] = "enabled"
		case "JSON":
			variables[varKey] = map[string]interface{}{"enabled": true}
		}
	}

	// If no variables, we'll just set the feature flag itself
	if len(variables) == 0 {
		variables[featureKey] = true
	}

	return CreateFeatureConfiguration(
		apiKey,
		projectKey,
		featureKey,
		record.Audience,
		variationKey,
		variables,
	)
}

func (api *devcycleAPI) checkAndCreateCustomProperties(dvcProject string, customData map[string]string) error {
	existingProps, err := api.getExistingCustomProperties(dvcProject)
	if err != nil {
		return fmt.Errorf("failed to get existing custom properties: %w", err)
	}
	for key, dataType := range customData {
		if _, exists := existingProps[key]; !exists {
			if err := api.createCustomProperty(dvcProject, key, dataType); err != nil {
				return fmt.Errorf("failed to create custom property %s: %w", key, err)
			}
			fmt.Printf("Created custom property: %s (%s)\n", key, dataType)
		} else {
			fmt.Printf("Custom property %s already exists\n", key)
		}
	}
	return nil
}
