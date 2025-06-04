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

func createTargetingRule(dvcProject, featureID string, tlFeature TLImportRecord) error {
	// Convert Taplytics filters to DevCycle targeting
	filters := convertTLFiltersToDevCycleTargeting(tlFeature.Audience.Filters)

	// Create rule payload
	rulePayload := map[string]interface{}{
		"name":     fmt.Sprintf("%s Rule", tlFeature.Audience.Name),
		"type":     "multivariate",
		"priority": 1,
		"filters":  filters,
		"variations": []map[string]interface{}{
			{
				"_variation": "treatment",
				"weight":     1.0,
			},
		},
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	jsonData, err := json.Marshal(rulePayload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST",
		fmt.Sprintf("https://api.devcycle.com/v1/projects/%s/features/%s/targeting", dvcProject, featureID),
		bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("DEVCYCLE_API_TOKEN")))

	resp, err := client.Do(req)
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

func createDevCycleFeature(dvcProject string, tlFeature TLImportRecord) error {
	// Generate a valid feature key from the name
	featureKey := toKey(tlFeature.FeatureName)

	// Set up variables
	variables := []DevCycleVariable{}
	for _, tlVar := range tlFeature.Variables {
		varType := convertTaplyticsVarTypeToDevCycle(tlVar.Type)
		varKey := toKey(tlVar.Name)

		variables = append(variables, DevCycleVariable{
			Name:        tlVar.Name,
			Key:         varKey,
			Type:        varType,
			Description: fmt.Sprintf("Imported from Taplytics: %s", tlVar.Name),
		})
	}

	variations := []DevCycleVariation{
		{
			Key:       "imported-on",
			Name:      "On",
			Variables: createVariationValues(variables, false),
		},
		{
			Key:       "imported-off",
			Name:      "Off",
			Variables: createVariationValues(variables, true),
		},
	}

	// Build feature payload
	featurePayload := DevCycleNewFeaturePOSTBody{
		Name:        tlFeature.FeatureName,
		Key:         featureKey,
		Description: fmt.Sprintf("Imported from Taplytics: %s", tlFeature.FeatureName),
		Variables:   variables,
		Variations:  variations,
		SdkVisibility: SdkVisibility{
			Mobile: true,
			Client: true,
			Server: true,
		},
		Type: "release", // Default to release type
		Tags: tlFeature.Tags,
	}

	// Create the feature
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	jsonData, err := json.Marshal(featurePayload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST",
		fmt.Sprintf("https://api.devcycle.com/v1/projects/%s/features", dvcProject),
		bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("DEVCYCLE_API_TOKEN")))

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API returned error %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response to get feature ID
	var createResponse struct {
		Data struct {
			ID string `json:"_id"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&createResponse); err != nil {
		return fmt.Errorf("failed to parse feature creation response: %w", err)
	}

	// Create targeting rules for the feature
	if len(tlFeature.Audience.Filters.Filters) > 0 {
		if err := createTargetingRule(dvcProject, createResponse.Data.ID, tlFeature); err != nil {
			return fmt.Errorf("failed to create targeting rules: %w", err)
		}
	}

	return nil
}

func importFeaturesToDevCycle(dvcProject string, mergedFeatures map[string]TLImportRecord) error {
	// First, ensure all required custom data properties exist
	customDataProps := make(map[string]string)
	for _, feature := range mergedFeatures {
		for _, filter := range feature.Audience.Filters.Filters {
			if filter.SubType == "customData" {
				customDataProps[filter.DataKey] = filter.DataKeyType
			}
		}
	}

	if err := checkAndCreateCustomProperties(dvcProject, customDataProps); err != nil {
		return fmt.Errorf("failed to set up custom properties: %w", err)
	}

	// Import each feature
	for _, feature := range mergedFeatures {
		if err := createDevCycleFeature(dvcProject, feature); err != nil {
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
