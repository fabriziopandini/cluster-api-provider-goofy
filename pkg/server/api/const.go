package api

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

var (
	// apiVersions is the value returned by /api discovery call.
	// Note: This must contain all the API are required by CAPI
	apiVersions = &metav1.APIVersions{
		Versions: []string{"v1"},
	}

	// apiVersions is the value returned by api/v1 discovery call.
	// Note: This must contain all the API are required by CAPI.
	apiResourceList = &metav1.APIResourceList{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{
			{
				Name:         "nodes",
				SingularName: "",
				Namespaced:   false,
				Kind:         "Node",
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
				ShortNames: []string{
					"no",
				},
				Categories:         nil,
				StorageVersionHash: "",
			},
			{
				Name:         "pods",
				SingularName: "",
				Namespaced:   true,
				Kind:         "Pod",
				Verbs: []string{
					"create",
					"delete",
					"deletecollection",
					"get",
					"list",
					"patch",
					"update",
					"watch",
				},
				ShortNames: []string{
					"po",
				},
				Categories: []string{
					"all",
				},
				StorageVersionHash: "",
			},
		},
	}

	// apiVersions is the value returned by apis discovery call.
	// Note: This must contain all the API are required by CAPI.
	apiGroupList = &metav1.APIGroupList{}
)

func resourceToKind(resource string) string {
	kind := ""
	for _, r := range apiResourceList.APIResources {
		if r.Name == resource {
			kind = r.Kind
			break
		}
	}
	return kind
}
