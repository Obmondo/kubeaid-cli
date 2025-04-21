# Workload Identity

Workloads deployed in Kubernetes clusters **require Azure AD application credentials** or managed identities to access Azure AD protected resources, such as Azure Key Vault and Microsoft Graph.

The Azure AD Pod Identity open-source project provided a way to avoid needing these secrets, by using Azure managed identities.

`Azure AD Workload Identity for Kubernetes` integrates with the capabilities native to Kubernetes to **federate with external identity providers**. This approach is simpler to use and deploy, and overcomes several limitations in Azure AD Pod Identity :

- Removes the scale and performance issues that existed for identity assignment

- Supports Kubernetes clusters hosted in any cloud or on-premises

- Supports both Linux and Windows workloads

- Removes the need for Custom Resource Definitions and pods that intercept Instance Metadata Service (IMDS) traffic

- Avoids the complication and error-prone installation steps such as cluster role assignment from the previous iteration.

In this model, the **Kubernetes cluster becomes a token issuer, issuing tokens to Kubernetes Service
Accounts. These service account tokens can be configured to be trusted on Azure AD applications or
user-assigned managed identities. A workload can exchange a service account token projected to its volume for an Azure AD access token** using the Azure Identity SDKs or the Microsoft Authentication Library (MSAL).

The workflow looks like this :

- The Kubernetes workload sends the signed ServiceAccount token in a request, to Azure Active Directory (AAD).

- AAD will then extract the OpenID provider issuer discovery document URL from the request and fetch it from Azure Storage Container.

- AAD will extract the JWKS document URL from that OpenID provider issuer discovery document and fetch it as well. The JSON Web Key Sets (JWKS) document contains the public signing key(s) that allows AAD to verify the authenticity of the service account token.

- AAD will use the public signing key(s) to verify the authenticity of the ServiceAccount token. And finally, it'll return an AAD token, back to the Kubernetes workload.

Refer to the sequence diagram [here](https://azure.github.io/azure-workload-identity/docs/installation/self-managed-clusters/oidc-issuer.html#sequence-diagram).

## Azure Federated Identity Credential

> Not all ServiceAccount tokens can be exchanged for a valid AAD (Azure Active Directory) token. A federated identity credential between an existing Kubernetes ServiceAccount and an AAD application or user-assigned managed identity has to be created in advance.

Traditionally, developers use certificates or client secrets for their application's credentials to authenticate with and access services in Microsoft Entra ID. To access the services in their Microsoft Entra tenant, developers had to store and manage application credentials outside Azure, introducing the following bottlenecks :

- A maintenance burden for certificates and secrets.

- The risk of leaking secrets.

- Certificates expiring and service disruptions because of failed authentication.

Federated identity credentials are a new type of credential that enables workload identity federation for software workloads. Workload identity federation allows you to access Microsoft Entra protected resources without needing to manage secrets (for supported scenarios).

You create a trust relationship between an external identity provider (IdP) and an app in Microsoft Entra ID by configuring a federated identity credential. The federated identity credential is used to indicate which token from the external IdP your application can trust. After that trust relationship is created, your software workload can exchange trusted tokens from the external identity provider for access tokens from the Microsoft identity platform. Your software workload then uses that access token to access the Microsoft Entra protected resources to which the workload has access.
