package resource

// ConfigOverrides holds data users can set to override default object configurations created
// by this operator. This is stored in the registry Config.Spec.UnsupportedConfigOverrides.
type ConfigOverrides struct {
	Deployment *DeploymentOverrides `json:"deployment,omitempty"`
}

// DeploymentOverrides holds items that can be overwriten in the image registry deployment.
type DeploymentOverrides struct {
	Annotations      map[string]string `json:"annotations,omitempty"`
	RuntimeClassName *string           `json:"runtimeClassName,omitempty"`
}
