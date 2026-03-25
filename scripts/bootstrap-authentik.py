#!/usr/bin/env python3
import json
import os

os.environ.setdefault("DJANGO_SETTINGS_MODULE", "authentik.settings")

import django

django.setup()

from authentik.core.models import Application
from authentik.crypto.models import CertificateKeyPair
from authentik.flows.models import Flow
from authentik.providers.oauth2.models import (
    OAuth2Provider,
    RedirectURI,
    RedirectURIMatchingMode,
    ScopeMapping,
)


runtime_audience = os.getenv("OCLI_RUNTIME_AUDIENCE", "oclird")
service_id = os.getenv("OCLI_RUNTIME_SERVICE_ID", "testapi")
provider_name = os.getenv("OCLI_AUTHENTIK_PROVIDER_NAME", "ocli Runtime Local Provider")
application_name = os.getenv("OCLI_AUTHENTIK_APPLICATION_NAME", "ocli Runtime Local")
client_slug = os.getenv("OCLI_AUTHENTIK_CLIENT_SLUG", "ocli-runtime-local")
redirect_uri = os.getenv("OCLI_AUTHENTIK_REDIRECT_URI", "http://127.0.0.1:8787/callback")
access_token_validity = os.getenv("OCLI_AUTHENTIK_ACCESS_TOKEN_VALIDITY", "hours=1")
client_type = os.getenv("OCLI_AUTHENTIK_CLIENT_TYPE", "confidential")
extra_scopes = [scope for scope in os.getenv("OCLI_RUNTIME_EXTRA_SCOPES", "").split() if scope]

scope_expression = (
    "audience = {audience!r}\n"
    'return {{"scope": " ".join(token.scope), "aud": audience}}\n'
).format(audience=runtime_audience)

auth_flow = Flow.objects.get(slug="default-authentication-flow")
authorization_flow = Flow.objects.get(slug="default-provider-authorization-implicit-consent")
invalidation_flow = Flow.objects.get(slug="default-provider-invalidation-flow")

signing_key = CertificateKeyPair.objects.filter(name="authentik Self-signed Certificate").first()
if signing_key is None:
    signing_key = CertificateKeyPair.objects.filter(name="authentik Internal JWT Certificate").first()
if signing_key is None:
    signing_key = CertificateKeyPair.objects.order_by("name").first()
if signing_key is None:
    raise RuntimeError("no signing key available in Authentik")


def ensure_scope(scope_name: str) -> ScopeMapping:
    mapping, _ = ScopeMapping.objects.update_or_create(
        name=f"ocli runtime {scope_name}",
        defaults={
            "scope_name": scope_name,
            "description": scope_name,
            "expression": scope_expression,
        },
    )
    return mapping


scope_names = [f"bundle:{service_id}", *extra_scopes]
scope_mappings = [ensure_scope(scope_name) for scope_name in scope_names]

provider = OAuth2Provider.objects.filter(name=provider_name).first()
if provider is None:
    provider = OAuth2Provider(name=provider_name)

provider.client_type = client_type
provider.include_claims_in_id_token = True
provider.access_token_validity = access_token_validity
provider.refresh_token_validity = "days=30"
provider.authentication_flow = auth_flow
provider.authorization_flow = authorization_flow
provider.invalidation_flow = invalidation_flow
provider.signing_key = signing_key
provider.redirect_uris = [
    RedirectURI(matching_mode=RedirectURIMatchingMode.STRICT, url=redirect_uri),
]
provider.save()
provider.property_mappings.set(scope_mappings)

application, _ = Application.objects.update_or_create(
    slug=client_slug,
    defaults={"name": application_name, "provider": provider},
)
if application.provider_id != provider.pk:
    application.provider = provider
    application.save(update_fields=["provider"])

result = {
    "slug": client_slug,
    "client_type": client_type,
    "client_id": provider.client_id,
    "client_secret": provider.client_secret,
    "scope_names": scope_names,
}
print("__OCLI_JSON__=" + json.dumps(result))
