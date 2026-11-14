# Privacy policy

AniList-Arr-Sync is a self-hosted application. The installation's operator chooses
the AniList username, Sonarr and Radarr connections, notification destinations,
and update settings. This policy describes the application and its embedded web
dashboard. Operators providing access to other people are responsible for their
own deployment and handling of those users' data.

## Data stored by an installation

Configuration and the SQLite database retain settings, service credentials,
AniList and media metadata, mapping decisions, synchronization history, and
notification connections. Administrator authentication data and browser sessions
are used to control access to the dashboard. Browser cookies support sign-in and
request protection; the browser also stores dashboard preferences such as theme.

Local backups contain configuration and database data. Logs can contain titles,
service URLs, and error details. Configuration, database, logs, and backups can
contain sensitive information and should be accessible only to the installation's
operators and authorized users. The application does not provide a central account
or upload an installation's database and backups to the project maintainer.

Data locations, backup retention, and service settings are controlled by the
operator. See [README.md](README.md#runtime-and-data) for the default locations.
Uninstalling the Windows application preserves its data directory. Container data
persists in the configured volume or bind mount. To remove retained data, stop the
application and remove the relevant data and backups, including any configured
storage paths outside the default directory. Browser data can be cleared through
the browser's site-data controls.

## Network requests

| Destination                                     | Purpose and information involved                                                                                                     |
| ----------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| AniList                                         | Query the configured username's anime lists and media metadata through its API.                                                      |
| GitHub Raw, Fribb anime-lists                   | Download community mappings used to match titles across services.                                                                    |
| Configured Sonarr and Radarr servers            | Read library and profile data and apply synchronization choices using the configured API credentials.                                |
| Configured Discord or generic webhook endpoints | Send selected notifications, which can include titles, media identifiers, synchronization results, and error details.                |
| GitHub API and release hosting                  | Discover releases and fetch update metadata and assets when update checks or installation are requested or enabled.                  |
| Image hosts referenced by media metadata        | Load cover images in the dashboard and notification previews. Fallback covers can use `dummyimage.com`.                            |
| Source, license, and documentation hosts        | Open the selected external page or source download when a user follows its link. These include GitHub, Go, and npm source locations. |

External services receive the connecting system's IP address and normal request
metadata. Dashboard image requests originate in the viewer's browser; notification
image requests may be made by the notification provider. The configured AniList
username is sent to AniList, while Sonarr and Radarr credentials are used for the
operator's configured servers. These servers may be local or remote.

Third-party services apply their own policies. See [AniList's terms](https://anilist.co/terms),
[GitHub's privacy statement](https://docs.github.com/en/site-policy/privacy-policies/github-general-privacy-statement), and [Discord's privacy policy](https://discord.com/privacy), together with the
policies of any custom service, image host, proxy, or notification destination
used by the deployment.

## Operator controls

The operator selects service connections and synchronization settings, enables
notification connections and triggers, selects the update channel, and controls
automatic updating. Automatic updating defaults to false. Choosing a notification
destination authorizes the selected events to be delivered there. Connection
changes control subsequent work; they do not retract requests already sent or
remove data already retained by a provider.

Authentication, trusted networks, TLS, reverse proxies, and network exposure are
deployment choices. The default configuration bypasses sign-in for configured
local networks, so operators should review access settings before sharing a
dashboard or exposing it beyond their intended network.

## Analytics, release infrastructure, and support

The application contains no analytics or telemetry integration that reports
installation activity to the project maintainer. Configured integrations, update
checks, image loading, and external links still generate the requests described
above. GitHub and signing providers process build and release artifacts during
distribution. The installed application does not send signing requests to SignPath.

GitHub issues and discussions are public. Remove API keys, passwords, webhook
URLs, personal data, and other sensitive information before sharing logs or
screenshots. The maintainer does not need production signing keys or service
credentials to investigate an issue. Questions about this project's behavior can
be directed to [the maintainer](https://github.com/lucasskywalker64); requests about
a hosted deployment's retained data belong to its operator.
