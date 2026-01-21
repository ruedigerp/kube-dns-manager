/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruedigerp/kube-dns-manager/dnsapi"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// IngressReconciler reconciles a Ingress object
type IngressReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	ConfigMapName      string
	ConfigMapNamespace string
}

// +kubebuilder:rbac:groups=networking.tytik.cloud,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.tytik.cloud,resources=ingresses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.tytik.cloud,resources=ingresses/finalizers,verbs=update

const ingressFinalizer = "kube-dns-manager.io/dns-cleanup"

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/reconcile
func (r *IngressReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Ingress-Resource laden
	var ingress networkingv1.Ingress
	if err := r.Get(ctx, req.NamespacedName, &ingress); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Ingress resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Ingress")
		return ctrl.Result{}, err
	}

	// Traefik-Konfiguration aus der ConfigMap laden
	traefikServiceName, traefikNamespace, excludeDomains, err := r.loadTraefikConfig(ctx)
	if err != nil {

		logger.Error(err, "Failed to load Traefik configuration")
		return ctrl.Result{}, err
	}

	logger.Info(fmt.Sprintf("Using Traefik service: %s/%s", traefikNamespace, traefikServiceName))

	// Handle finalizer logic
	if ingress.DeletionTimestamp.IsZero() {

		// Add finalizer if not already present
		if err := r.addFinalizer(ctx, &ingress); err != nil {
			logger.Error(err, "Failed to add finalizer to ingress")
			return ctrl.Result{}, err
		}

	} else {
		// Handle cleanup during deletion
		if containsString(ingress.Finalizers, ingressFinalizer) {

			logger.Info("Cleaning up DNS records for deleted Ingress")

			domains := r.extractDomains(&ingress)

			filteredDomains := []string{}

			dnsconfig, _ := r.loadDNSConfiguration(ctx, ingress.Annotations["dns.configuration/source"])
			for _, domain := range domains {

				if containsString(filteredDomains, domain) {
					fmt.Printf("Domain %s is included in filteredDomains.\n", domain)
					continue
				}

				aexists, id, _ := dnsapi.GetRecordId(dnsconfig["zoneid"], dnsconfig["token"], domain, "A")
				if aexists {
					if astate, err := dnsapi.DeleteRecord(dnsconfig["zoneid"], dnsconfig["token"], id); err != nil {
						logger.Error(err, "Failed to delete A record", "domain", domain)
						if !astate {
							fmt.Printf("Record not deleted")
						}
					}
				}

				txtexists, id, _ := dnsapi.GetRecordId(dnsconfig["zoneid"], dnsconfig["token"], domain, "txt")
				if txtexists {
					if txtstate, err := dnsapi.DeleteRecord(dnsconfig["zoneid"], dnsconfig["token"], id); err != nil {
						logger.Error(err, "Failed to delete TXT record", "domain", domain)
						if !txtstate {
							fmt.Printf("Record not deleted")
						}
					}
				}
			}

			// Remove the finalizer
			if err := r.removeFinalizer(ctx, &ingress); err != nil {
				logger.Error(err, "Failed to remove finalizer from ingress")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, nil
	}
	// LoadBalancer-IP des Traefik-Service abrufen
	loadBalancerIP, err := r.getLoadBalancerIP(ctx, traefikNamespace, traefikServiceName)
	if err != nil {
		logger.Error(err, "Failed to get LoadBalancer IP")
		return ctrl.Result{}, err
	}

	logger.Info(fmt.Sprintf("LoadBalancer IP for Traefik: %s", loadBalancerIP))

	// Annotationen prüfen
	typeAnnotationKey := "dns.configuration/type"
	typeAnnotationValue, found := ingress.Annotations[typeAnnotationKey]
	if !found {
		logger.Info("No DNS configuration type annotation found. Skipping...")
		return ctrl.Result{}, nil
	}

	sourceAnnotationKey := "dns.configuration/source"
	sourceAnnotationValue, found := ingress.Annotations[sourceAnnotationKey]
	if !found {
		logger.Info("No DNS configuration source annotation found. Skipping...")
		return ctrl.Result{}, nil
	}
	logger.Info("Source Annotation: ", "sourceAnnotationValue", sourceAnnotationValue)

	// Extract current domains
	currentDomains := r.extractDomains(&ingress)

	// Prüfen, ob die Domänen in der Exclude-Liste sind
	filteredDomains := []string{}
	for _, domain := range currentDomains {
		if !containsString(excludeDomains, domain) {
			filteredDomains = append(filteredDomains, domain)
		} else {
			logger.Info("Domain excluded from processing", "domain", domain)
		}
	}

	// Falls keine Domänen übrig bleiben, abbrechen
	if len(filteredDomains) == 0 {
		logger.Info("All domains are excluded. Skipping processing for Ingress")
		return ctrl.Result{}, nil
	}

	// Load previous domains from annotation
	previousDomainsKey := "dns.configuration/previous-domains"

	var previousDomains []string
	if val, found := ingress.Annotations[previousDomainsKey]; found {
		previousDomains = strings.Split(val, ",")
	} else {
		previousDomains = []string{}
	}

	removedDomains := difference(previousDomains, currentDomains)

	addDomains := difference(currentDomains, previousDomains)

	switch typeAnnotationValue {
	case "bind":

		logger.Info("Handle domains for BIND", "domains", removedDomains)
	case "cloudflare":

		dnsconfig, _ := r.loadDNSConfiguration(ctx, sourceAnnotationValue)

		for _, domain := range removedDomains {

			if containsString(filteredDomains, domain) {
				fmt.Printf("Domain %s ist in filteredDomains enthalten.\n", domain)
				continue
			}

			exists, id, _ := dnsapi.GetRecordId(dnsconfig["zoneid"], dnsconfig["token"], domain, "A")
			if exists {
				// logger.Info("DNS Record exists", "id", id)
				dnsapi.DeleteRecord(dnsconfig["zoneid"], dnsconfig["token"], id)
			}

			txtexists, id, _ := dnsapi.GetRecordId(dnsconfig["zoneid"], dnsconfig["token"], domain, "TXT")
			if txtexists {
				dnsapi.DeleteRecord(dnsconfig["zoneid"], dnsconfig["token"], id)
			} else {
				fmt.Printf("TXT Record for %s not exists.\n", domain)
			}

		}
	default:

		logger.Info(fmt.Sprintf("Unknown DNS configuration type: %s. Skipping...", typeAnnotationValue))

	}
	// Add records
	switch typeAnnotationValue {
	case "bind":

		logger.Info("Handle add domains for BIND", "domains", addDomains)

	case "cloudflare":

		dnsconfig, err := r.loadDNSConfiguration(ctx, sourceAnnotationValue)

		if err != nil {
			logger.Info("Fehler beim lesen der Config", "err", err)
			return ctrl.Result{}, err
		}

		for _, domain := range currentDomains {

			if !containsString(filteredDomains, domain) {
				fmt.Printf("Domain %s is included in filteredDomains.\n", domain)
				continue
			}

			exists, _, _ := dnsapi.GetRecordId(dnsconfig["zoneid"], dnsconfig["token"], domain, "a")
			if exists {
				dnsapi.UpdateRecord(dnsconfig["zoneid"], dnsconfig["token"], domain, "A", loadBalancerIP, false)
			} else {
				dnsapi.AddRecord(dnsconfig["zoneid"], dnsconfig["token"], domain, "A", loadBalancerIP, false)
			}

			txtexists, _, _ := dnsapi.GetRecordId(dnsconfig["zoneid"], dnsconfig["token"], domain, "TXT")
			if txtexists {
				dnsapi.UpdateRecord(dnsconfig["zoneid"], dnsconfig["token"], domain, "TXT", "kube-dns-manager", false)
			} else {
				dnsapi.AddRecord(dnsconfig["zoneid"], dnsconfig["token"], domain, "TXT", "kube-dns-manager", false)
			}
		}
	default:
		logger.Info(fmt.Sprintf("Unknown DNS configuration type: %s. Skipping...", typeAnnotationValue))
	}

	// Update the annotation with current domains
	ingress.Annotations[previousDomainsKey] = strings.Join(currentDomains, ",")
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := r.Get(ctx, client.ObjectKeyFromObject(&ingress), &ingress); err != nil {
			return err
		}
		DomainList := difference(currentDomains, excludeDomains)
		// ingress.Annotations[previousDomainsKey] = strings.Join(currentDomains, ",")
		ingress.Annotations[previousDomainsKey] = strings.Join(DomainList, ",")
		return r.Update(ctx, &ingress)
	})

	if retryErr != nil {
		logger.Error(retryErr, "Failed to update ingress annotations")
		return ctrl.Result{}, retryErr
	}

	return ctrl.Result{}, nil
}

func difference(slice1, slice2 []string) []string {
	// slice1: domainliste
	// slice2: ExcludeDomains
	// filteredDomains := difference(domainliste, ExcludeDomains)

	m := make(map[string]bool)
	for _, v := range slice2 {
		m[v] = true
	}

	var diff []string
	for _, v := range slice1 {
		if !m[v] {
			diff = append(diff, v)
		}
	}

	return diff

}

func containsString(slice []string, str string) bool {

	for _, v := range slice {
		if v == str {
			return true
		}
	}

	return false

}

// Load DNS configuration from ConfigMap or Secret
func (r *IngressReconciler) loadDNSConfiguration(ctx context.Context, sourceName string) (map[string]string, error) {
	config := make(map[string]string)

	namespace := r.ConfigMapNamespace

	// Try to load as ConfigMap
	var configMap corev1.ConfigMap
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: sourceName}, &configMap); err == nil {
		for key, value := range configMap.Data {
			config[key] = value
		}
		return config, nil
	}

	// Try to load as Secret
	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: sourceName}, &secret); err == nil {
		for key, value := range secret.Data {
			config[key] = string(value)
		}
		return config, nil
	}

	return nil, fmt.Errorf("configuration source %s not found as ConfigMap or Secret in namespace %s", sourceName, namespace)
}

// Konfiguration aus der ConfigMap laden
func (r *IngressReconciler) loadTraefikConfig(ctx context.Context) (string, string, []string, error) {

	var configMap corev1.ConfigMap

	err := r.Get(ctx, client.ObjectKey{
		Namespace: r.ConfigMapNamespace,
		Name:      r.ConfigMapName,
	}, &configMap)

	if err != nil {
		if errors.IsNotFound(err) {
			// Standardwerte, wenn die ConfigMap nicht gefunden wird
			return "traefik", "kube-system", nil, nil
		}
		return "", "", nil, fmt.Errorf("failed to load ConfigMap: %w", err)
	}

	// Standardwerte für Traefik
	traefikServiceName := configMap.Data["traefikServiceName"]
	if traefikServiceName == "" {
		traefikServiceName = "traefik"
	}

	traefikNamespace := configMap.Data["traefikNamespace"]
	if traefikNamespace == "" {
		traefikNamespace = "kube-system"
	}

	// ExcludeDomains aus YAML laden
	var excludeDomains []string
	if excludeDomainsRaw, found := configMap.Data["excludedomains"]; found {
		if err := yaml.Unmarshal([]byte(excludeDomainsRaw), &excludeDomains); err != nil {
			return "", "", nil, fmt.Errorf("failed to parse excludedomains: %w", err)
		}
	}

	return traefikServiceName, traefikNamespace, excludeDomains, nil
}

// LoadBalancer-IP des Traefik-Service abrufen
func (r *IngressReconciler) getLoadBalancerIP(ctx context.Context, namespace string, serviceName string) (string, error) {

	var service corev1.Service

	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: serviceName}, &service); err != nil {
		return "", fmt.Errorf("failed to get service %s/%s: %w", namespace, serviceName, err)
	}

	// LoadBalancer-IP oder Hostname prüfen
	if len(service.Status.LoadBalancer.Ingress) > 0 {
		ingress := service.Status.LoadBalancer.Ingress[0]
		if ingress.IP != "" {
			return ingress.IP, nil
		}
		if ingress.Hostname != "" {
			return ingress.Hostname, nil
		}
	}

	return "", fmt.Errorf("no LoadBalancer IP or hostname found for service %s/%s", namespace, serviceName)
}

// Domains aus dem Ingress-Spec extrahieren
func (r *IngressReconciler) extractDomains(ingress *networkingv1.Ingress) []string {

	var domains []string

	for _, rule := range ingress.Spec.Rules {
		domains = append(domains, rule.Host)
	}

	return domains
}

func (r *IngressReconciler) SetupWithManager(mgr ctrl.Manager) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1.Ingress{}).
		Named("ingress").
		Complete(r)
}

func removeString(slice []string, str string) []string {

	var result []string

	for _, v := range slice {
		if v != str {
			result = append(result, v)
		}
	}

	return result
}

// AddFinalizer ensures that the finalizer is added safely
func (r *IngressReconciler) addFinalizer(ctx context.Context, ingress *networkingv1.Ingress) error {

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Erneutes Abrufen des neuesten Objekts

		if err := r.Get(ctx, client.ObjectKeyFromObject(ingress), ingress); err != nil {
			return err
		}

		// Finalizer hinzufügen, falls noch nicht vorhanden
		if !containsString(ingress.Finalizers, ingressFinalizer) {
			ingress.Finalizers = append(ingress.Finalizers, ingressFinalizer)
			if err := r.Update(ctx, ingress); err != nil {
				return err
			}
		}

		return nil

	})

}

// RemoveFinalizer ensures that the finalizer is removed safely
func (r *IngressReconciler) removeFinalizer(ctx context.Context, ingress *networkingv1.Ingress) error {

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {

		// Erneutes Abrufen des neuesten Objekts
		if err := r.Get(ctx, client.ObjectKeyFromObject(ingress), ingress); err != nil {
			return err
		}

		// Finalizer entfernen, falls vorhanden
		if containsString(ingress.Finalizers, ingressFinalizer) {
			ingress.Finalizers = removeString(ingress.Finalizers, ingressFinalizer)
			if err := r.Update(ctx, ingress); err != nil {
				return err
			}
		}

		return nil

	})

}
