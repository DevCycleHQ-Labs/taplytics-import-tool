package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

var nonAlphanumericRegex = regexp.MustCompile(`[^a-zA-Z0-9\-_ ]+`)

func clearString(str string) string {
	return str
}

func getDefaultValue(variableType string, state bool) any {
	switch strings.ToLower(variableType) {
	case "string":
		return ""
	case "number":
		if state {
			return 1
		}
		return 0
	case "boolean":
		return state
	default:
		return ""
	}
}
func main() {
	var dvcProject string
	// Read in the first argument as the file path
	filePath := os.Args[1]

	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Println("Error reading file:", err)
		return
	}

	var tlImport TLImportFormat
	err = json.Unmarshal(fileContent, &tlImport)
	if err != nil {
		fmt.Println("Error parsing file:", err)
		return
	}
	if tlImport.TLProject == "" {
		fmt.Println("TLProject is required")
		return
	} else {
		dvcProject = tlImport.DVCProject
	}

	var cleanedTLImport []TLImportRecord
	// Clean up the feature names for merging
	for _, feature := range tlImport.Records {
		if len(feature.Variables) == 0 {
			continue
		}
		cleanedTLImport = append(cleanedTLImport, feature)
	}

	mergedFeatures := make(map[string]TLImportRecord)
	for _, feature := range cleanedTLImport {
		if _, ok := mergedFeatures[feature.FeatureName]; !ok {
			mergedFeatures[feature.FeatureName] = feature
		} else {
			if mergedFeatures[feature.FeatureName].FeatureName != feature.FeatureName {
				merged := mergedFeatures[feature.FeatureName]
				merged.Variables = append(merged.Variables, feature.Variables...)
				mergedFeatures[feature.FeatureName] = merged
			}
		}
	}

	fmt.Println("Importing", len(mergedFeatures), "features from Taplytics to DevCycle")
	fmt.Println("DevCycle Project:", dvcProject)
	fmt.Println("CustomData Properties:", tlImport.GetCustomDataProperties())

	// Now create features with targeting rules
	if err = importFeaturesToDevCycle(dvcProject, mergedFeatures); err != nil {
		fmt.Printf("Error importing features: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Import completed successfully!")
}

func checkAndCreateCustomProperties(dvcProject string, customData map[string]string) error {
	// Get existing custom properties
	existingProps, err := getExistingCustomProperties(dvcProject)
	if err != nil {
		return fmt.Errorf("failed to get existing custom properties: %w", err)
	}

	// Create any missing properties
	for key, dataType := range customData {
		if _, exists := existingProps[key]; !exists {
			if err := createCustomProperty(dvcProject, key, dataType); err != nil {
				return fmt.Errorf("failed to create custom property %s: %w", key, err)
			}
			fmt.Printf("Created custom property: %s (%s)\n", key, dataType)
		} else {
			fmt.Printf("Custom property %s already exists\n", key)
		}
	}

	return nil
}

func getExistingCustomProperties(dvcProject string) (map[string]struct{}, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET",
		fmt.Sprintf("https://api.devcycle.com/v1/projects/%s/custom-properties", dvcProject),
		nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("DEVCYCLE_API_TOKEN")))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned error %d: %s", resp.StatusCode, string(body))
	}

	// We're intentionally ignoring the rest of the properties because we only care about the keys for dedupe
	var response struct {
		Data []struct {
			Key string `json:"key"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	// Create map of property keys for quick lookup
	result := make(map[string]struct{})
	for _, prop := range response.Data {
		result[prop.Key] = struct{}{}
	}

	return result, nil
}

func createCustomProperty(dvcProject, key, dataType string) error {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Map Taplytics data types to DevCycle types
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

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST",
		fmt.Sprintf("https://api.devcycle.com/v1/projects/%s/custom-properties", dvcProject),
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
