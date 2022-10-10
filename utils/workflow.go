package utils

import (
	"context"
	"encoding/json"
	apiv08 "github.com/davidesalerno/kogito-serverless-operator/api/v08"
	"github.com/davidesalerno/kogito-serverless-operator/converters"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

func GetWorkflowFromCR(workflowCR *apiv08.KogitoServerlessWorkflow, ctx context.Context) ([]byte, error) {
	log := ctrllog.FromContext(ctx)
	converter := converters.NewKogitoServerlessWorkflowConverter(ctx)
	workflow, err := converter.ToCNCFWorkflow(workflowCR)
	if err != nil {
		log.Error(err, "Failed converting KogitoServerlessWorkflow into Workflow")
		return nil, err
	}
	jsonWorkflow, err := json.Marshal(workflow)
	if err != nil {
		log.Error(err, "Failed converting KogitoServerlessWorkflow into JSON")
		return nil, err
	}
	return jsonWorkflow, nil
}
