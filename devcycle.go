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
	featureKey := generateFeatureKey(tlFeature.FeatureName)

	// Set up variables
	variables := []DevCycleVariable{}
	for _, tlVar := range tlFeature.Variables {
		varType := convertTaplyticsVarTypeToDevCycle(tlVar.Type)
		varKey := generateFeatureKey(tlVar.Name)

		variables = append(variables, DevCycleVariable{
			Name:        tlVar.Name,
			Key:         varKey,
			Type:        varType,
			Description: fmt.Sprintf("Imported from Taplytics: %s", tlVar.Name),
		})
	}

	// Create variations (always need at least "control" and "treatment" variations)
	variations := []DevCycleVariation{
		{
			Key:       "control",
			Name:      "Control",
			Variables: createVariationValues(variables, false),
		},
		{
			Key:       "treatment",
			Name:      "Treatment",
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
