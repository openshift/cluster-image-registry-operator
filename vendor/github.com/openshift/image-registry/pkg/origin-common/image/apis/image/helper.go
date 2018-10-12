package image

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/opencontainers/go-digest"
)

const (
	// DockerDefaultNamespace is the value for namespace when a single segment name is provided.
	DockerDefaultNamespace = "library"
	// DockerDefaultRegistry is the value for the registry when none was provided.
	DockerDefaultRegistry = "docker.io"
	// DockerDefaultV1Registry is the host name of the default v1 registry
	DockerDefaultV1Registry = "index." + DockerDefaultRegistry
	// DockerDefaultV2Registry is the host name of the default v2 registry
	DockerDefaultV2Registry = "registry-1." + DockerDefaultRegistry
)

// ParseImageStreamImageName splits a string into its name component and ID component, and returns an error
// if the string is not in the right form.
func ParseImageStreamImageName(input string) (name string, id string, err error) {
	segments := strings.SplitN(input, "@", 3)
	switch len(segments) {
	case 2:
		name = segments[0]
		id = segments[1]
		if len(name) == 0 || len(id) == 0 {
			err = fmt.Errorf("image stream image name %q must have a name and ID", input)
		}
	default:
		err = fmt.Errorf("expected exactly one @ in the isimage name %q", input)
	}
	return
}

// IsRegistryDockerHub returns true if the given registry name belongs to
// Docker hub.
func IsRegistryDockerHub(registry string) bool {
	switch registry {
	case DockerDefaultRegistry, DockerDefaultV1Registry, DockerDefaultV2Registry:
		return true
	default:
		return false
	}
}

// ParseDockerImageReference parses a Docker pull spec string into a
// DockerImageReference.
func ParseDockerImageReference(spec string) (DockerImageReference, error) {
	var ref DockerImageReference

	namedRef, err := parseNamedDockerImageReference(spec)
	if err != nil {
		return ref, err
	}

	ref.Registry = namedRef.Registry
	ref.Namespace = namedRef.Namespace
	ref.Name = namedRef.Name
	ref.Tag = namedRef.Tag
	ref.ID = namedRef.ID

	return ref, nil
}

// DockerClientDefaults sets the default values used by the Docker client.
func (r DockerImageReference) DockerClientDefaults() DockerImageReference {
	if len(r.Registry) == 0 {
		r.Registry = DockerDefaultRegistry
	}
	if len(r.Namespace) == 0 && IsRegistryDockerHub(r.Registry) {
		r.Namespace = DockerDefaultNamespace
	}
	if len(r.Tag) == 0 {
		r.Tag = DefaultImageTag
	}
	return r
}

// AsRepository returns the reference without tags or IDs.
func (r DockerImageReference) AsRepository() DockerImageReference {
	r.Tag = ""
	r.ID = ""
	return r
}

// RepositoryName returns the registry relative name
func (r DockerImageReference) RepositoryName() string {
	r.Tag = ""
	r.ID = ""
	r.Registry = ""
	return r.Exact()
}

// RepositoryName returns the registry relative name
func (r DockerImageReference) RegistryURL() *url.URL {
	return &url.URL{
		Scheme: "https",
		Host:   r.AsV2().Registry,
	}
}

func (r DockerImageReference) AsV2() DockerImageReference {
	switch r.Registry {
	case DockerDefaultV1Registry, DockerDefaultRegistry:
		r.Registry = DockerDefaultV2Registry
	}
	return r
}

// NameString returns the name of the reference with its tag or ID.
func (r DockerImageReference) NameString() string {
	switch {
	case len(r.Name) == 0:
		return ""
	case len(r.Tag) > 0:
		return r.Name + ":" + r.Tag
	case len(r.ID) > 0:
		var ref string
		if _, err := digest.Parse(r.ID); err == nil {
			// if it parses as a digest, its v2 pull by id
			ref = "@" + r.ID
		} else {
			// if it doesn't parse as a digest, it's presumably a v1 registry by-id tag
			ref = ":" + r.ID
		}
		return r.Name + ref
	default:
		return r.Name
	}
}

// Exact returns a string representation of the set fields on the DockerImageReference
func (r DockerImageReference) Exact() string {
	name := r.NameString()
	if len(name) == 0 {
		return name
	}
	s := r.Registry
	if len(s) > 0 {
		s += "/"
	}

	if len(r.Namespace) != 0 {
		s += r.Namespace + "/"
	}
	return s + name
}

// String converts a DockerImageReference to a Docker pull spec (which implies a default namespace
// according to V1 Docker registry rules). Use Exact() if you want no defaulting.
func (r DockerImageReference) String() string {
	if len(r.Namespace) == 0 && IsRegistryDockerHub(r.Registry) {
		r.Namespace = DockerDefaultNamespace
	}
	return r.Exact()
}

// SplitImageStreamTag turns the name of an ImageStreamTag into Name and Tag.
// It returns false if the tag was not properly specified in the name.
func SplitImageStreamTag(nameAndTag string) (name string, tag string, ok bool) {
	parts := strings.SplitN(nameAndTag, ":", 2)
	name = parts[0]
	if len(parts) > 1 {
		tag = parts[1]
	}
	if len(tag) == 0 {
		tag = DefaultImageTag
	}
	return name, tag, len(parts) == 2
}

// JoinImageStreamTag turns a name and tag into the name of an ImageStreamTag
func JoinImageStreamTag(name, tag string) string {
	if len(tag) == 0 {
		tag = DefaultImageTag
	}
	return fmt.Sprintf("%s:%s", name, tag)
}

// JoinImageStreamImage creates a name for image stream image object from an image stream name and an id.
func JoinImageStreamImage(name, id string) string {
	return fmt.Sprintf("%s@%s", name, id)
}

// DigestOrImageMatch matches the digest in the image name.
func DigestOrImageMatch(image, imageID string) bool {
	if d, err := digest.Parse(image); err == nil {
		return strings.HasPrefix(d.Hex(), imageID) || strings.HasPrefix(image, imageID)
	}
	return strings.HasPrefix(image, imageID)
}
