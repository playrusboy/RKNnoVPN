package ipc

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"sync"
)

//go:embed contract_manifest.json
var contractManifestJSON []byte

var (
	manifestOnce sync.Once
	manifestData Contract
	manifestErr  error
	hashOnce     sync.Once
	hashValue    string
)

type OperationContract struct {
	Type           string   `json:"type"`
	AsyncResultVia string   `json:"asyncResultVia,omitempty"`
	Stages         []string `json:"stages,omitempty"`
}

type MethodContract struct {
	Method        string             `json:"method"`
	Capability    string             `json:"capability,omitempty"`
	Mutating      bool               `json:"mutating"`
	Async         bool               `json:"async"`
	Request       string             `json:"request"`
	Result        string             `json:"result"`
	ErrorCodes    []string           `json:"errorCodes,omitempty"`
	Operation     *OperationContract `json:"operation,omitempty"`
	Compatibility string             `json:"compatibility,omitempty"`
}

type OperationPolicy struct {
	Stages     []string `json:"stages"`
	ErrorCodes []string `json:"errorCodes"`
}

type Contract struct {
	Version                int                        `json:"version"`
	ControlProtocolVersion int                        `json:"controlProtocolVersion"`
	SchemaVersion          int                        `json:"schemaVersion"`
	ErrorCodes             []string                   `json:"errorCodes,omitempty"`
	CompatibilityPolicies  []string                   `json:"compatibilityPolicies,omitempty"`
	RequestExamples        map[string]json.RawMessage `json:"requestExamples,omitempty"`
	OperationPolicies      map[string]OperationPolicy `json:"operationPolicies,omitempty"`
	Capabilities           []string                   `json:"capabilities"`
	APKRequiredMethods     []string                   `json:"apkRequiredMethods,omitempty"`
	Methods                []MethodContract           `json:"methods"`
}

func SupportedMethods() []string {
	contracts := MethodContracts()
	methods := make([]string, 0, len(contracts))
	for _, contract := range contracts {
		methods = append(methods, contract.Method)
	}
	sort.Strings(methods)
	return methods
}

func SupportedCapabilities() []string {
	capabilities := append([]string(nil), contractManifest().Capabilities...)
	sort.Strings(capabilities)
	return capabilities
}

func APKRequiredMethods() []string {
	methods := append([]string(nil), contractManifest().APKRequiredMethods...)
	sort.Strings(methods)
	return methods
}

func ErrorCodes() []string {
	return append([]string(nil), contractManifest().ErrorCodes...)
}

func CompatibilityPolicies() []string {
	return append([]string(nil), contractManifest().CompatibilityPolicies...)
}

func OperationPolicies() map[string]OperationPolicy {
	return cloneOperationPolicies(contractManifest().OperationPolicies)
}

func RequestExample(request string) (json.RawMessage, bool) {
	if request == "" {
		return nil, false
	}
	example, ok := contractManifest().RequestExamples[request]
	if !ok {
		return nil, false
	}
	return cloneRawMessage(example), true
}

func OperationPolicyForType(operationType string) (OperationPolicy, bool) {
	if operationType == "" {
		return OperationPolicy{}, false
	}
	policy, ok := contractManifest().OperationPolicies[operationType]
	if !ok {
		return OperationPolicy{}, false
	}
	return OperationPolicy{
		Stages:     append([]string(nil), policy.Stages...),
		ErrorCodes: append([]string(nil), policy.ErrorCodes...),
	}, true
}

func MethodContracts() []MethodContract {
	return cloneMethodContracts(contractManifest().Methods)
}

func MethodOperationType(method string) (string, bool) {
	if method == "" {
		return "", false
	}
	for _, contract := range contractManifest().Methods {
		if contract.Method != method {
			continue
		}
		if contract.Operation == nil || contract.Operation.Type == "" {
			return "", false
		}
		return contract.Operation.Type, true
	}
	return "", false
}

func ContractVersion() int {
	return contractManifest().Version
}

func ContractHash() string {
	hashOnce.Do(func() {
		sum := sha256.Sum256(contractManifestJSON)
		hashValue = fmt.Sprintf("%x", sum[:])
	})
	return hashValue
}

func NewContract(controlProtocolVersion int, schemaVersion int, capabilities []string) Contract {
	copiedCapabilities := append([]string(nil), capabilities...)
	sort.Strings(copiedCapabilities)
	return Contract{
		Version:                contractManifest().Version,
		ControlProtocolVersion: controlProtocolVersion,
		SchemaVersion:          schemaVersion,
		ErrorCodes:             append([]string(nil), contractManifest().ErrorCodes...),
		CompatibilityPolicies:  append([]string(nil), contractManifest().CompatibilityPolicies...),
		RequestExamples:        cloneRequestExamples(contractManifest().RequestExamples),
		OperationPolicies:      cloneOperationPolicies(contractManifest().OperationPolicies),
		Capabilities:           copiedCapabilities,
		APKRequiredMethods:     APKRequiredMethods(),
		Methods:                MethodContracts(),
	}
}

func contractManifest() Contract {
	manifestOnce.Do(func() {
		manifestErr = json.Unmarshal(contractManifestJSON, &manifestData)
		if manifestData.Version == 0 {
			manifestData.Version = 1
		}
		if manifestErr == nil {
			manifestData, manifestErr = completeContractManifest(manifestData)
		}
		if manifestErr == nil {
			manifestErr = validateContractManifest(manifestData)
		}
	})
	if manifestErr != nil {
		panic(manifestErr)
	}
	return manifestData
}

func completeContractManifest(manifest Contract) (Contract, error) {
	completed := manifest
	completed.Methods = cloneMethodContracts(manifest.Methods)
	for i := range completed.Methods {
		method := &completed.Methods[i]
		if method.Operation == nil {
			continue
		}
		policy, ok := manifest.OperationPolicies[method.Operation.Type]
		if !ok {
			continue
		}
		if len(method.ErrorCodes) > 0 && !slices.Equal(method.ErrorCodes, policy.ErrorCodes) {
			return Contract{}, fmt.Errorf("ipc contract method %s error codes drifted: got %v, expected %v", method.Method, method.ErrorCodes, policy.ErrorCodes)
		}
		if len(method.Operation.Stages) > 0 && !slices.Equal(method.Operation.Stages, policy.Stages) {
			return Contract{}, fmt.Errorf("ipc contract method %s operation stages drifted: got %v, expected %v", method.Method, method.Operation.Stages, policy.Stages)
		}
		method.ErrorCodes = append([]string(nil), policy.ErrorCodes...)
		method.Operation.Stages = append([]string(nil), policy.Stages...)
	}
	return completed, nil
}

func validateContractManifest(manifest Contract) error {
	if manifest.Version < 1 {
		return fmt.Errorf("ipc contract manifest version must be positive")
	}
	if len(manifest.Methods) == 0 {
		return fmt.Errorf("ipc contract manifest must declare methods")
	}

	errorCodes := make(map[string]bool, len(manifest.ErrorCodes))
	for _, code := range manifest.ErrorCodes {
		if code == "" {
			return fmt.Errorf("ipc contract manifest contains an empty error code")
		}
		if errorCodes[code] {
			return fmt.Errorf("ipc contract manifest contains duplicate error code %q", code)
		}
		errorCodes[code] = true
	}
	if len(errorCodes) == 0 {
		return fmt.Errorf("ipc contract manifest must declare error codes")
	}
	if expected := PublicErrorCodeNames(); !slices.Equal(manifest.ErrorCodes, expected) {
		return fmt.Errorf("ipc contract manifest error codes drifted: got %v, expected %v", manifest.ErrorCodes, expected)
	}

	compatibilityPolicies := make(map[string]bool, len(manifest.CompatibilityPolicies))
	for _, policy := range manifest.CompatibilityPolicies {
		if policy == "" {
			return fmt.Errorf("ipc contract manifest contains an empty compatibility policy")
		}
		if compatibilityPolicies[policy] {
			return fmt.Errorf("ipc contract manifest contains duplicate compatibility policy %q", policy)
		}
		compatibilityPolicies[policy] = true
	}
	if len(compatibilityPolicies) == 0 {
		return fmt.Errorf("ipc contract manifest must declare compatibility policies")
	}

	capabilities := make(map[string]bool, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		if capability == "" {
			return fmt.Errorf("ipc contract manifest contains an empty capability")
		}
		if capabilities[capability] {
			return fmt.Errorf("ipc contract manifest contains duplicate capability %q", capability)
		}
		capabilities[capability] = true
	}

	requestSchemas := make(map[string]bool, len(manifest.Methods)+1)
	requestSchemas["empty"] = true
	for _, method := range manifest.Methods {
		if method.Request != "" {
			requestSchemas[method.Request] = true
		}
	}
	for request, example := range manifest.RequestExamples {
		if request == "" {
			return fmt.Errorf("ipc contract manifest contains an empty request example key")
		}
		if !requestSchemas[request] {
			return fmt.Errorf("ipc contract request example %s does not match any request schema", request)
		}
		if !json.Valid(example) {
			return fmt.Errorf("ipc contract request example %s is not valid JSON", request)
		}
	}

	for operationType, policy := range manifest.OperationPolicies {
		if operationType == "" {
			return fmt.Errorf("ipc contract manifest contains an operation policy without type")
		}
		if len(policy.Stages) == 0 {
			return fmt.Errorf("ipc contract operation policy %s has no stages", operationType)
		}
		if hasDuplicates(policy.Stages) {
			return fmt.Errorf("ipc contract operation policy %s contains duplicate stages", operationType)
		}
		if len(policy.ErrorCodes) == 0 {
			return fmt.Errorf("ipc contract operation policy %s has no error codes", operationType)
		}
		if hasDuplicates(policy.ErrorCodes) {
			return fmt.Errorf("ipc contract operation policy %s contains duplicate error codes", operationType)
		}
		for _, code := range policy.ErrorCodes {
			if !errorCodes[code] {
				return fmt.Errorf("ipc contract operation policy %s references undeclared error code %s", operationType, code)
			}
		}
	}

	methods := make(map[string]bool, len(manifest.Methods))
	usedOperationPolicies := make(map[string]bool, len(manifest.OperationPolicies))
	for _, method := range manifest.Methods {
		if method.Method == "" {
			return fmt.Errorf("ipc contract manifest contains a method without a name")
		}
		if methods[method.Method] {
			return fmt.Errorf("ipc contract manifest contains duplicate method %q", method.Method)
		}
		methods[method.Method] = true
		if method.Capability == "" {
			return fmt.Errorf("ipc contract method %s has no capability", method.Method)
		}
		if !capabilities[method.Capability] {
			return fmt.Errorf("ipc contract method %s references undeclared capability %s", method.Method, method.Capability)
		}
		if method.Compatibility != "" && !compatibilityPolicies[method.Compatibility] {
			return fmt.Errorf("ipc contract method %s uses unknown compatibility policy %s", method.Method, method.Compatibility)
		}
		if method.Request == "" {
			return fmt.Errorf("ipc contract method %s has no request schema", method.Method)
		}
		if method.Result == "" {
			return fmt.Errorf("ipc contract method %s has no result schema", method.Method)
		}
		if len(method.ErrorCodes) == 0 {
			return fmt.Errorf("ipc contract method %s has no error codes", method.Method)
		}
		for _, code := range method.ErrorCodes {
			if !errorCodes[code] {
				return fmt.Errorf("ipc contract method %s declares unknown error code %s", method.Method, code)
			}
		}
		if method.Mutating && method.Operation == nil {
			return fmt.Errorf("ipc contract mutating method %s has no operation metadata", method.Method)
		}
		if method.Async && (method.Operation == nil || method.Operation.AsyncResultVia == "") {
			return fmt.Errorf("ipc contract async method %s has no async result surface", method.Method)
		}
		if method.Operation != nil {
			if method.Operation.Type == "" {
				return fmt.Errorf("ipc contract method %s has operation metadata without type", method.Method)
			}
			if len(method.Operation.Stages) == 0 {
				return fmt.Errorf("ipc contract method %s has operation metadata without stages", method.Method)
			}
			policy, ok := manifest.OperationPolicies[method.Operation.Type]
			if !ok {
				return fmt.Errorf("ipc contract method %s uses operation type %s without stage policy", method.Method, method.Operation.Type)
			}
			if !slices.Equal(method.Operation.Stages, policy.Stages) {
				return fmt.Errorf("ipc contract method %s operation stages drifted: got %v, expected %v", method.Method, method.Operation.Stages, policy.Stages)
			}
			if !slices.Equal(method.ErrorCodes, policy.ErrorCodes) {
				return fmt.Errorf("ipc contract method %s error codes drifted: got %v, expected %v", method.Method, method.ErrorCodes, policy.ErrorCodes)
			}
			usedOperationPolicies[method.Operation.Type] = true
		}
	}
	for operationType := range manifest.OperationPolicies {
		if !usedOperationPolicies[operationType] {
			return fmt.Errorf("ipc contract operation policy %s is not used by any method", operationType)
		}
	}

	required := make(map[string]bool, len(manifest.APKRequiredMethods))
	for _, method := range manifest.APKRequiredMethods {
		if method == "" {
			return fmt.Errorf("ipc contract apk required methods contain an empty method")
		}
		if required[method] {
			return fmt.Errorf("ipc contract apk required methods contain duplicate method %q", method)
		}
		if !methods[method] {
			return fmt.Errorf("ipc contract apk required method %s is not declared", method)
		}
		required[method] = true
	}
	return nil
}

func hasDuplicates(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}

func cloneMethodContracts(methods []MethodContract) []MethodContract {
	copied := make([]MethodContract, len(methods))
	for i, method := range methods {
		copied[i] = method
		copied[i].ErrorCodes = append([]string(nil), method.ErrorCodes...)
		if method.Operation != nil {
			operation := *method.Operation
			operation.Stages = append([]string(nil), method.Operation.Stages...)
			copied[i].Operation = &operation
		}
	}
	return copied
}

func cloneOperationPolicies(policies map[string]OperationPolicy) map[string]OperationPolicy {
	if len(policies) == 0 {
		return nil
	}
	copied := make(map[string]OperationPolicy, len(policies))
	for operationType, policy := range policies {
		copied[operationType] = OperationPolicy{
			Stages:     append([]string(nil), policy.Stages...),
			ErrorCodes: append([]string(nil), policy.ErrorCodes...),
		}
	}
	return copied
}

func cloneRequestExamples(examples map[string]json.RawMessage) map[string]json.RawMessage {
	if len(examples) == 0 {
		return nil
	}
	copied := make(map[string]json.RawMessage, len(examples))
	for request, example := range examples {
		copied[request] = cloneRawMessage(example)
	}
	return copied
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}
