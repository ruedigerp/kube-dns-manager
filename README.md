# Ingress DNS Operator

The Ingress DNS Operator automatically manages DNS records for Kubernetes Ingress resources by integrating with external DNS providers like Cloudflare and BIND. It updates DNS records when new ingress resources are created or modified, and removes DNS records when ingress resources are deleted. This operator leverages annotations on Ingress objects to determine configuration details, such as the DNS provider and related settings.

## Features

	1.	Dynamic DNS Record Management
	•	Automatically creates, updates, or deletes DNS records for Ingress resources.
	2.	Traefik Integration
	•	Retrieves the LoadBalancer IP of a Traefik service for use in DNS records.
	3.	Support for Multiple DNS Providers
	•	Works with Cloudflare and BIND via annotations.
	4.	Finalizer Mechanism
	•	Ensures proper cleanup of DNS records when ingress resources are deleted.
	5.	Exclude Domains
	•	Configurable list of domains to exclude from DNS management.

# Ingress Configuration

To enable DNS management, you must annotate your Ingress resources with the following keys:

## Required Annotations

| Key	                    | Value	                        | Description
|---------------------------|-------------------------------|---------------------------------------|
| dns.configuration/type	| cloudflare or bind	        | The type of DNS provider to use. |
| dns.configuration/source	| <configmap-or-secret-name>	| The name of the ConfigMap or Secret containing DNS provider credentials. |

## Example Ingress

    apiVersion: networking.k8s.io/v1  
    kind: Ingress  
    metadata:  
      name: example-ingress  
      annotations:  
        dns.configuration/type: "cloudflare"  
        dns.configuration/source: "example-com-dns-config"  
    spec:  
      rules:  
        - host: "example.com"  
          http:  
            paths:  
              - path: "/"  
                pathType: ImplementationSpecific  
                backend:  
                  service:  
                    name: example-service  
                    port:  
                      number: 80  

# Operator Configuration

The operator reads settings from a ConfigMap that provides details about the Traefik service and the list of excluded domains.

## ConfigMap

| Key	              | Description	                                                        | Default Value |
|---------------------|---------------------------------------------------------------------|---------------|
| traefikServiceName  | The name of the Traefik service whose LoadBalancer IP will be used.	| traefik       |
| traefikNamespace	  |  The namespace where the Traefik service is located.	            | kube-system   |
| excludeDomains	  |  A YAML array of domains to exclude from DNS management.	        | None          |

### Example ConfigMap

    apiVersion: v1  
    kind: ConfigMap  
    metadata:  
      name: dns-operator-config  
      namespace: kube-system  
    data:  
      traefikServiceName: "traefik"  
      traefikNamespace: "kube-system"  
      excludeDomains: |  
        - "excluded-domain.com"  
        - "another-excluded.com"  

# DNS Provider Configurations

The operator uses either a ConfigMap or a Secret to store credentials and configuration for the DNS provider.

## Example for Cloudflare

### ConfigMap

    apiVersion: v1  
    kind: ConfigMap  
    metadata:  
      name: example-com-dns-config
      namespace: kube-system  
    data:  
      token: "<cloudflare-api-token>"  
      zoneid: "<cloudflare-zone-id>"  

### Secret

    apiVersion: v1  
    kind: Secret  
    metadata:  
      name: dns-config  
      namespace: kube-system  
    type: Opaque  
    data:  
      token: "<base64-cloudflare-api-token>"  
      zoneid: "<base64-cloudflare-zone-id>"  

## Example for BIND

### ConfigMap

    apiVersion: v1  
    kind: ConfigMap  
    metadata:  
      name: dns-config  
      namespace: kube-system  
    data:  
      bindServer: "bind-server.example.com"  
      bindPort: "53" 
      hmackey: "abcdefg1234567890"
      zone: "example.com" 

# Operator Workflow

	1.	Create or Update Ingress
	•	Extracts the domains from the ingress rules.
	•	Filters excluded domains.
	•	Retrieves the LoadBalancer IP from the Traefik service.
	•	Adds or updates DNS A and TXT records.
	2.	Delete Ingress
	•	Uses a finalizer to clean up associated DNS records.
	•	Removes A and TXT records for the ingress domains.

# Known Limitations

	•	Only supports A and TXT DNS records.
	•	Requires manual setup of the ConfigMap and Secret for DNS providers.

This README provides a comprehensive guide to setting up and using the Ingress DNS Operator. For additional details or support, feel free to contact the project maintainers.


# DNS Operator für Ingress-Objekte

Der DNS Operator automatisiert die Verwaltung von DNS-Einträgen für Kubernetes-Ingress-Ressourcen. Basierend auf benutzerdefinierten Annotationen und Konfigurationen erstellt, aktualisiert oder entfernt der Operator DNS-Einträge für spezifische Domains.

## Funktionalität des Operators

	1.	Verarbeiten von Ingress-Ressourcen:
Der Operator beobachtet Ingress-Ressourcen im Cluster und reagiert auf Änderungen oder neue Ressourcen.
	2.	DNS-Typen:
Der Operator unterstützt zwei DNS-Typen, die über die Annotation dns.configuration/type festgelegt werden:
	•	bind: Erstellt und verwaltet DNS-Einträge über BIND.
	•	cloudflare: Verwendet die Cloudflare-API zur Verwaltung von DNS-Einträgen.
	3.	Quellen für DNS-Konfiguration:
Die spezifischen Konfigurationen für den DNS-Server werden aus einer ConfigMap oder einem Secret geladen. Die Quelle wird über die Annotation dns.configuration/source definiert.
	4.	LoadBalancer-IP:
Der Operator extrahiert die LoadBalancer-IP des Traefik-Services und verwendet diese für die Erstellung der DNS-Einträge.
	5.	Finalizer:
Der Operator fügt einen Finalizer zu Ingress-Objekten hinzu, um sicherzustellen, dass die DNS-Einträge vor der endgültigen Löschung der Ressource entfernt werden.
	6.	Domain-Filter:
Domains können durch die ConfigMap-Einstellungen ausgeschlossen werden.

## Annotationen für Ingress-Ressourcen

Die folgenden Annotationen sind erforderlich, um den Operator zu konfigurieren:


| Key	                    | Value	                        | Description |
|---------------------------|-------------------------------|---------------------------------------|
| dns.configuration/type	| cloudflare oder bind	        | Gibt den Type des DNS Providers an.   |
| dns.configuration/source	| <configmap-or-secret-name>	| Definiert die Quelle der DNS-Konfiguration. Dies ist der Name einer ConfigMap oder eines Secrets, das die erforderlichen Zugangsdaten enthält. |

Vom Operator verwendete Annotation zur Nachverfolgung von Domains, die bereits verarbeitet wurden.

## ConfigMap für den Operator

Der Operator benötigt eine zentrale ConfigMap, um grundlegende Einstellungen zu laden.

### Name der ConfigMap: dns-operator-config

Beispiel-Inhalt:

  apiVersion: v1
  kind: ConfigMap
  metadata:
    name: dns-operator-config
    namespace: kube-system
  data:
    traefikServiceName: "traefik"
    traefikNamespace: "kube-system"
    excludedomains: |
      - "excluded-domain.com"
      - "another-excluded-domain.com"

### Erläuterung:

| Key	              | Description	                                              | Default Value |
|---------------------|-----------------------------------------------------------|---------------|
| traefikServiceName  | Name des Traefik-Services                                 | traefik |
| traefikNamespace    | Namespace des Traefik-Services                            | kube-system |
| excludedomains      | YAML-Liste von Domains, die der Operator ignorieren soll. | |

## ConfigMap oder Secret für DNS-Konfiguration

Je nach dns.configuration/source müssen entweder eine ConfigMap oder ein Secret mit den DNS-Zugangsdaten bereitgestellt werden.

## Beispiel: ConfigMap für Cloudflare

  apiVersion: v1
  kind: ConfigMap
  metadata:
    name: cloudflare-config
    namespace: kube-system
  data:
    zoneid: "<ZONE_ID>"
    token: "<API_TOKEN>"

## Beispiel: Secret für BIND

    apiVersion: v1  
    kind: ConfigMap  
    metadata:  
      name: dns-config  
      namespace: kube-system  
    data:  
      bindServer: "bind-server.example.com"  
      bindPort: "53" 
      hmackey: "abcdefg1234567890"
      zone: "example.com"

## Beispiel-Ingress-Ressource

### Cloudflare-basierte DNS-Einträge

    apiVersion: networking.k8s.io/v1
    kind: Ingress
    metadata:
      name: example-ingress
      namespace: default
    annotations:
      dns.configuration/type: "cloudflare"
      dns.configuration/source: "cloudflare-config"
    spec:
      rules:
        - host: "example.com"
          http:
            paths:
              - path: "/"
                pathType: Prefix
                backend:
                  service:
                    name: example-service
                    port:
                      number: 80

### BIND-basierte DNS-Einträge

    apiVersion: networking.k8s.io/v1
    kind: Ingress
    metadata:
      name: example-ingress
      namespace: default
      annotations:
        dns.configuration/type: "bind"
        dns.configuration/source: "bind-config"
    spec:
      rules:
        - host: "example.org"
          http:
            paths:
              - path: "/"
                pathType: Prefix
                backend:
                  service:
                    name: example-service
                    port:
                      number: 80

# Wie der Operator funktioniert

	1.	Beobachtung von Ingress-Ressourcen:
Der Operator beobachtet Ingress-Objekte im Cluster.
	2.	Prüfung der Annotationen:
Der Operator verarbeitet nur Ingress-Ressourcen mit gültigen DNS-Annotationen.
	3.	DNS-Einträge aktualisieren:
	•	Neue Domains werden hinzugefügt.
	•	Nicht mehr verwendete Domains werden entfernt.
	4.	Finalizer-Verwaltung:
Vor dem Löschen eines Ingress-Objekts werden alle zugehörigen DNS-Einträge entfernt.
	5.	LoadBalancer-IP abrufen:
Die IP des Traefik-LoadBalancers wird aus dem Service-Status geladen und für DNS-Einträge verwendet.

# Voraussetzungen

	1.	Ein funktionierender Kubernetes-Cluster.
	2.	Ein installierter und konfigurierter Traefik-LoadBalancer.
	3.	Die ConfigMap dns-operator-config.
	4.	Ggf. weitere ConfigMaps oder Secrets für DNS-Provider (z. B. Cloudflare oder BIND).

