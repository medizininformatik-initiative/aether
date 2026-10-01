package services

// capabilityStatement holds the parts of a CapabilityStatement that identify a
// service: the software name for TORCH and the operations for the flattener.
type capabilityStatement struct {
	ResourceType string `json:"resourceType"`
	Software     struct {
		Name string `json:"name"`
	} `json:"software"`
	Rest []struct {
		Resource []struct {
			Type      string `json:"type"`
			Operation []struct {
				Name string `json:"name"`
			} `json:"operation"`
		} `json:"resource"`
	} `json:"rest"`
}

// declaresOperation reports whether the statement declares the operation name
// on the resource type resourceType.
func (cs capabilityStatement) declaresOperation(resourceType, name string) bool {
	for _, rest := range cs.Rest {
		for _, resource := range rest.Resource {
			if resource.Type != resourceType {
				continue
			}
			for _, op := range resource.Operation {
				if op.Name == name {
					return true
				}
			}
		}
	}
	return false
}
