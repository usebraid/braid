---
title: OIDC providers
order: 4
group: Run the server
description: Sign in with Pocket ID, Authentik, Keycloak, Authelia, Zitadel or any OIDC provider.
---

# OIDC providers

Braid logs users in with any OpenID Connect provider that supports the authorization code flow with PKCE and a confidential client. Pocket ID is bundled in the Compose file; the others work the same way.

## Settings that are the same everywhere

| Setting | Value |
|---|---|
| Redirect URI | `<BRAID_PUBLIC_URL>/auth/callback` |
| Client type | confidential (client ID and secret) |
| Scopes | `openid profile email groups` (the default of `BRAID_OIDC_SCOPES`) |
| Issuer | `BRAID_OIDC_ISSUER`, the URL under which `/.well-known/openid-configuration` is served |
| Groups claim | `groups`, a flat claim holding a list of group names (`BRAID_OIDC_GROUPS_CLAIM` changes the name) |
| Admins | members of the group named in `BRAID_OIDC_ADMIN_GROUP` (default `braid-admins`) |
| Allowlist | `BRAID_OIDC_ALLOWED_GROUPS`, comma-separated; empty lets every user of the provider in, admins are always let in |

Set `BRAID_OIDC_CLIENT_ID` and `BRAID_OIDC_CLIENT_SECRET` from the client you create. Create the group `braid-admins` in your provider and add yourself. The claim must be a JSON list of strings (a single string also works); nested claims are not read.

## Pocket ID

Issuer: the URL Pocket ID is served from, for example `https://id.example.com` (no trailing path). Create the user group, create an OIDC client with the redirect URI above and assign the group to the client. Pocket ID releases the `groups` claim when the `groups` scope is requested.

## Authentik

Issuer: `https://authentik.example.com/application/o/<application slug>/`, shown on the provider page as "OpenID Configuration Issuer". Create an OAuth2/OpenID provider (confidential) with the redirect URI above and an application for it. Authentik's default scope mappings can emit a `groups` claim; if your instance does not, add a property mapping that returns the user's group names as a list under the key `groups` and select it in the provider's scopes.

## Keycloak

Issuer: `https://keycloak.example.com/realms/<realm>`. Create an OpenID Connect client with "Client authentication" on and the redirect URI above. Keycloak does not emit groups by default: add a "Group Membership" mapper to the client (or a client scope you assign to it), set the token claim name to `groups`, turn "Full group path" off (otherwise names get a leading `/`) and make sure it is added to the ID token.

## Authelia

Issuer: the URL Authelia is served from, for example `https://auth.example.com`. Add a client under `identity_providers.oidc.clients` with the redirect URI above, `scopes` including `openid`, `profile`, `email` and `groups`, and a client secret in the digest format Authelia asks for. The `groups` scope makes Authelia emit the user's groups as the `groups` claim.

## Zitadel

Issuer: the domain of your Zitadel instance, for example `https://zitadel.example.com`. Create a web application with authentication method "Basic" (confidential, code flow) and the redirect URI above. Zitadel has no plain groups claim: it puts project roles in its own claim as an object, which Braid does not read. Add an Action that writes a list of names into a claim, and set `BRAID_OIDC_GROUPS_CLAIM` to the name of that claim.

## Any other provider

Emit the group names as a list in the ID token under any claim name and set that name in `BRAID_OIDC_GROUPS_CLAIM`. If logins fail with "access denied", the user's groups do not match `BRAID_OIDC_ALLOWED_GROUPS`; the audit log shows `login_denied` with the reason.
