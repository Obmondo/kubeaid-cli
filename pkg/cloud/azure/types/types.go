package types

type (
	WorkloadIdentityInfrastructureStatus struct {
		CAPIUAMIPrincipalID string `json:"capiUAMIPrincipalId"`
	}

	DisasterRecoveryInfrastructureStatus struct {
		VeleroUAMIPrincipalID string `json:"veleroUAMIPrincipalId"`
	}
)
