package coreutil

import (
	"fmt"
	"reflect"
	"strings"

	coreapi "k8s.io/api/core/v1"
)

func GetVolumeSourceField(source coreapi.VolumeSource) (reflect.StructField, error) {
	val := reflect.ValueOf(source)
	var fields []reflect.StructField
	for i := 0; i < val.NumField(); i++ {
		if !val.Field(i).IsNil() {
			fields = append(fields, val.Type().Field(i))
		}
	}
	if len(fields) == 0 {
		return reflect.StructField{}, fmt.Errorf("the volume source does not have any sources")
	}
	if len(fields) > 1 {
		names := make([]string, 0, len(fields))
		for _, field := range fields {
			names = append(names, field.Name)
		}
		return reflect.StructField{}, fmt.Errorf("too many sources for the volume found: %s", strings.Join(names, ", "))
	}
	return fields[0], nil
}
