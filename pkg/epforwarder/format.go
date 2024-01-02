// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package epforwarder

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DataDog/agent-payload/v5/contimage"
	"github.com/DataDog/agent-payload/v5/contlcycle"
	"github.com/DataDog/agent-payload/v5/sbom"
	"google.golang.org/protobuf/proto"

	"github.com/DataDog/datadog-agent/pkg/logs/message"
)

// epFormatter extends diagnostic.Formatter and is used to format the various protobuf and json payloads which
// are generated by this package, for diagnostic purposes.
type epFormatter struct{}

func (e *epFormatter) Format(m *message.Message, eventType string, _ []byte) string {
	var output strings.Builder
	output.WriteString(fmt.Sprintf("type: %v | ", eventType))

	switch eventType {
	case EventTypeContainerLifecycle:
		var msg contlcycle.EventsPayload
		if err := proto.Unmarshal(m.GetContent(), &msg); err != nil {
			output.WriteString(err.Error())
		} else {
			prettyPrint(&output, &msg)
		}
	case EventTypeContainerImages:
		var msg contimage.ContainerImagePayload
		if err := proto.Unmarshal(m.GetContent(), &msg); err != nil {
			output.WriteString(err.Error())
		} else {
			prettyPrint(&output, &msg)
		}
	case EventTypeContainerSBOM:
		var msg sbom.SBOMPayload
		if err := proto.Unmarshal(m.GetContent(), &msg); err != nil {
			output.WriteString(err.Error())
		} else {
			prettyPrint(&output, &msg)
		}
	default:
		output.Write(m.GetContent())
	}
	output.WriteRune('\n')
	return output.String()
}

func prettyPrint(sb *strings.Builder, v any) {
	encoder := json.NewEncoder(sb)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		sb.WriteString(err.Error())
	}
}
