package main

import (
	"log"
	"strconv"
	"strings"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func logRuntimeOperation(event runtimev2.OperationLogEvent) {
	fields := []string{
		"runtime_operation",
		"operation=" + string(event.Kind),
		"generation=" + strconv.FormatInt(event.Generation, 10),
		"phase=" + string(event.Phase),
		"result=" + event.Result,
	}
	if event.RuntimeMS > 0 {
		fields = append(fields, "runtime_ms="+strconv.FormatInt(event.RuntimeMS, 10))
	}
	if event.Step != "" {
		fields = append(fields, "step="+event.Step)
	}
	if event.StepStatus != "" {
		fields = append(fields, "step_status="+event.StepStatus)
	}
	if event.StepDetail != "" {
		fields = append(fields, "step_detail="+strconv.Quote(event.StepDetail))
	}
	if event.ErrorCode != "" {
		fields = append(fields, "code="+event.ErrorCode)
	}
	if event.Stuck {
		fields = append(fields, "stuck=true")
	}
	if event.ErrorMessage != "" {
		fields = append(fields, "error="+strconv.Quote(event.ErrorMessage))
	}
	log.Print(strings.Join(fields, " "))
}
