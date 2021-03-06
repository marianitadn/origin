package config

import (
	"encoding/json"
	"fmt"
	"reflect"

	kubeapi "github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/errors"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/runtime"

	clientapi "github.com/openshift/origin/pkg/cmd/client/api"
	"github.com/openshift/origin/pkg/config/api"
)

// Apply creates and manages resources defined in the Config. It wont stop on
// error, but it will finish the job and then return list of errors.
//
// TODO: Return the output for each resource on success, so the client can
//       print it out.
func Apply(data []byte, storage clientapi.ClientMappings) (errs errors.ErrorList) {

	// Unmarshal the Config JSON manually instead of using runtime.Decode()
	conf := struct {
		Items []json.RawMessage `json:"items" yaml:"items"`
	}{}
	if err := json.Unmarshal(data, &conf); err != nil {
		return append(errs, fmt.Errorf("Unable to parse Config: %v", err))
	}

	if len(conf.Items) == 0 {
		return append(errs, fmt.Errorf("Config.items is empty"))
	}

	for i, item := range conf.Items {
		if item == nil || (len(item) == 4 && string(item) == "null") {
			errs = append(errs, fmt.Errorf("Config.items[%v] is null", i))
			continue
		}

		_, kind, err := runtime.VersionAndKind(item)
		if err != nil {
			errs = append(errs, fmt.Errorf("Config.items[%v]: %v", i, err))
			continue
		}

		if kind == "" {
			errs = append(errs, fmt.Errorf("Config.items[%v] has an empty 'kind'", i))
			continue
		}

		client, path, err := getClientAndPath(kind, storage)
		if err != nil {
			errs = append(errs, fmt.Errorf("Config.items[%v]: %v", i, err))
			continue
		}
		if client == nil {
			errs = append(errs, fmt.Errorf("Config.items[%v]: Invalid client for 'kind=%v'", i, kind))
			continue
		}

		jsonResource, err := item.MarshalJSON()
		if err != nil {
			errs = append(errs, err)
			continue
		}

		request := client.Verb("POST").Path(path).Body(jsonResource)
		_, err = request.Do().Get()
		if err != nil {
			errs = append(errs, fmt.Errorf("Failed to create Config.items[%v] of 'kind=%v': %v", i, kind, err))
		}
	}

	return
}

// AddConfigLabels adds new label(s) to all resources defined in the given Config.
func AddConfigLabels(c *api.Config, labels labels.Set) error {
	for i, _ := range c.Items {
		switch t := c.Items[i].Object.(type) {
		case *kubeapi.Pod:
			if err := mergeMaps(&t.Labels, labels, ErrorOnDifferentDstKeyValue); err != nil {
				return fmt.Errorf("Unable to add labels to Template.Items[%v] Pod.Labels: %v", i, err)
			}
		case *kubeapi.Service:
			if err := mergeMaps(&t.Labels, labels, ErrorOnDifferentDstKeyValue); err != nil {
				return fmt.Errorf("Unable to add labels to Template.Items[%v] Service.Labels: %v", i, err)
			}
		case *kubeapi.ReplicationController:
			if err := mergeMaps(&t.Labels, labels, ErrorOnDifferentDstKeyValue); err != nil {
				return fmt.Errorf("Unable to add labels to Template.Items[%v] ReplicationController.Labels: %v", i, err)
			}
			if err := mergeMaps(&t.DesiredState.PodTemplate.Labels, labels, ErrorOnDifferentDstKeyValue); err != nil {
				return fmt.Errorf("Unable to add labels to Template.Items[%v] ReplicationController.DesiredState.PodTemplate.Labels: %v", i, err)
			}
		default:
			// Unknown generic object. Try to find "Labels" field in it.
			obj := reflect.ValueOf(c.Items[i].Object)

			if obj.Kind() == reflect.Interface || obj.Kind() == reflect.Ptr {
				obj = obj.Elem()
			}
			if obj.Kind() != reflect.Struct {
				return fmt.Errorf("Template.Items[%v]: Invalid object kind. Expected: Struct, got:", i, obj.Kind())
			}

			obj = obj.FieldByName("Labels")
			if obj.IsValid() {
				// Merge labels into the Template.Items[i].Labels field.
				if err := mergeMaps(obj.Interface(), labels, ErrorOnDifferentDstKeyValue); err != nil {
					return fmt.Errorf("Unable to add labels to Template.Items[%v] GenericObject.Labels: %v", i, err)
				}
			}
		}
	}

	return nil
}

// mergeMaps flags
const (
	OverwriteExistingDstKey     = 1 << iota
	ErrorOnExistingDstKey       = 1 << iota
	ErrorOnDifferentDstKeyValue = 1 << iota
)

// mergeMaps merges items from a src map into a dst map.
// Returns an error when the maps are not of the same type.
// Flags:
// - ErrorOnExistingDstKey
//     When set: Return an error if any of the dst keys is already set.
// - ErrorOnDifferentDstKeyValue
//     When set: Return an error if any of the dst keys is already set
//               to a different value than src key.
// - OverwriteDstKey
//     When set: Overwrite existing dst key value with src key value.
func mergeMaps(dst, src interface{}, flags int) error {
	dstVal := reflect.ValueOf(dst)
	srcVal := reflect.ValueOf(src)

	if dstVal.Kind() == reflect.Interface || dstVal.Kind() == reflect.Ptr {
		dstVal = dstVal.Elem()
	}
	if srcVal.Kind() == reflect.Interface || srcVal.Kind() == reflect.Ptr {
		srcVal = srcVal.Elem()
	}

	if !dstVal.IsValid() {
		return fmt.Errorf("Dst is not a valid value")
	}
	if dstVal.Kind() != reflect.Map {
		return fmt.Errorf("Dst is not a map")
	}

	dstTyp := dstVal.Type()
	srcTyp := srcVal.Type()
	if !dstTyp.AssignableTo(srcTyp) {
		return fmt.Errorf("Type mismatch, can't assign '%v' to '%v'", srcTyp, dstTyp)
	}

	if dstVal.IsNil() {
		if !dstVal.CanSet() {
			return fmt.Errorf("Dst value is (not addressable) nil, pass a pointer instead")
		}
		dstVal.Set(reflect.MakeMap(dstTyp))
	}

	for _, k := range srcVal.MapKeys() {
		if dstVal.MapIndex(k).IsValid() {
			if flags&ErrorOnExistingDstKey != 0 {
				return fmt.Errorf("ErrorOnExistingDstKey flag: Dst key already set to a different value, '%v'='%v'", k, dstVal.MapIndex(k))
			}
			if dstVal.MapIndex(k).String() != srcVal.MapIndex(k).String() {
				if flags&ErrorOnDifferentDstKeyValue != 0 {
					return fmt.Errorf("ErrorOnDifferentDstKeyValue flag: Dst key already set to a different value, '%v'='%v'", k, dstVal.MapIndex(k))
				}
				if flags&OverwriteExistingDstKey != 0 {
					dstVal.SetMapIndex(k, srcVal.MapIndex(k))
				}
			}
		} else {
			dstVal.SetMapIndex(k, srcVal.MapIndex(k))
		}
	}

	return nil
}

// getClientAndPath returns the RESTClient and path defined for a given
// resource kind. Returns an error when no RESTClient is found.
func getClientAndPath(kind string, mappings clientapi.ClientMappings) (clientapi.RESTClient, string, error) {
	for k, m := range mappings {
		if m.Kind == kind {
			return m.Client, k, nil
		}
	}
	return nil, "", fmt.Errorf("No client found for 'kind=%v'", kind)
}

// reportError provides a human-readable error message that include the Config
// item JSON representation.
func reportError(item interface{}, message string) error {
	itemJSON, _ := json.Marshal(item)
	return fmt.Errorf(message+": %s", string(itemJSON))
}
