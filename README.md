# WP Packages

![Total Installs](https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fwp-packages.org%2Fapi%2Fstats&query=%24.total_installs&label=composer%20installs&style=flat-square)
[![Build status](https://img.shields.io/github/actions/workflow/status/roots/wp-packages/ci.yml?branch=main&style=flat-square)](https://github.com/roots/wp-packages/actions)
[![Follow Roots](https://img.shields.io/badge/follow%20@rootswp-1da1f2?logo=twitter&logoColor=ffffff&message=&style=flat-square)](https://twitter.com/rootswp)
[![Sponsor Roots](https://img.shields.io/badge/sponsor%20roots-525ddc?logo=github&style=flat-square&logoColor=ffffff&message=)](https://github.com/sponsors/roots)

Manage your WordPress plugins and themes with Composer.

## Support us

Roots is an independent open source org, supported only by developers like you. Your sponsorship funds [WP Packages](https://wp-packages.org/) and the entire Roots ecosystem, and keeps them independent. Support us by purchasing [Radicle](https://roots.io/radicle/) or [sponsoring us on GitHub](https://github.com/sponsors/roots) — sponsors get access to our private Discord.

### Sponsors

<a href="https://carrot.com/"><img src="https://cdn.roots.io/app/uploads/carrot.svg" alt="Carrot" width="120" height="90"></a> <a href="https://wordpress.com/"><img src="https://cdn.roots.io/app/uploads/wordpress.svg" alt="WordPress.com" width="120" height="90"></a> <a href="https://www.itineris.co.uk/"><img src="https://cdn.roots.io/app/uploads/itineris.svg" alt="Itineris" width="120" height="90"></a> <a href="https://kinsta.com/?kaid=OFDHAJIXUDIV"><img src="https://cdn.roots.io/app/uploads/kinsta.svg" alt="Kinsta" width="120" height="90"></a>

## [WP Packages vs WPackagist](https://wp-packages.org/wp-packages-vs-wpackagist)

|  | WP Packages | WPackagist |
|---|---|---|
| Package naming | `wp-plugin/*` `wp-theme/*` | `wpackagist-plugin/*` `wpackagist-theme/*` |
| Package metadata | Includes authors, description, homepage, and support links | Missing — [requested since 2020](https://github.com/outlandishideas/wpackagist/issues/305) |
| Update frequency | Every 5 minutes | ~1.5 hours (estimated) |
| Composer v2 `metadata-url` | ✅ | ❌ |
| Composer v2 `metadata-changes-url` | ✅ | ❌ |

### Composer resolve times

Cold resolve (no cache) — lower is better:

| Plugins | WP Packages | WPackagist | Speedup |
|---|---|---|---|
| 10 plugins | 0.7s | 12.3s | 17x faster |
| 20 plugins | 1.1s | 19.0s | 17x faster |

## Package Naming

| Type | Convention | Example |
|---|---|---|
| Plugin | `wp-plugin/plugin-name` | `wp-plugin/woocommerce` |
| Theme | `wp-theme/theme-name` | `wp-theme/twentytwentyfive` |

## Usage

Add the repository to your `composer.json`:

```json
{
  "repositories": [
    {
      "name": "wp-packages",
      "type": "composer",
      "url": "https://repo.wp-packages.org"
    }
  ],
  "require": {
    "composer/installers": "^2.2",
    "wp-plugin/woocommerce": "^10.0",
    "wp-theme/twentytwentyfive": "^1.0"
  },
  "extra": {
    "installer-paths": {
      "web/app/mu-plugins/{$name}/": ["type:wordpress-muplugin"],
      "web/app/plugins/{$name}/": ["type:wordpress-plugin"],
      "web/app/themes/{$name}/": ["type:wordpress-theme"]
    },
    "wordpress-install-dir": "web/wp"
  },
  "config": {
    "allow-plugins": {
      "composer/installers": true,
      "roots/wordpress-core-installer": true
    }
  }
}
```

- [`composer/installers`](https://github.com/composer/installers) — installs plugins and themes into their correct WordPress directories instead of `vendor/`
- `extra.installer-paths` — maps package types to your WordPress content directory (adjust paths to match your project structure)
- `extra.wordpress-install-dir` — tells [`roots/wordpress-core-installer`](https://github.com/roots/wordpress-core-installer) where to install WordPress core

### Roots WordPress Packages

WP Packages is built by [Roots](https://roots.io) and is the recommended repository for use alongside the Roots WordPress packaging ecosystem:

| Package | Description |
|---|---|
| [`roots/wordpress`](https://github.com/roots/wordpress) | Meta-package for installing WordPress core via Composer |
| [`roots/wordpress-full`](https://github.com/roots/wordpress-full) | Full WordPress build (core + default themes + plugins + betas) |
| [`roots/wordpress-no-content`](https://github.com/roots/wordpress-no-content) | Minimal WordPress build (core only) |
| [`roots/bedrock`](https://github.com/roots/bedrock) | WordPress boilerplate with Composer, better config, and improved structure |

A typical [Bedrock](https://roots.io/bedrock/) project uses `roots/wordpress` for WordPress core and WP Packages for plugins and themes:

```json
{
  "repositories": [
    {
      "name": "wp-packages",
      "type": "composer",
      "url": "https://repo.wp-packages.org"
    }
  ],
  "require": {
    "composer/installers": "^2.2",
    "roots/wordpress": "^6.9",
    "wp-plugin/woocommerce": "^10.0",
    "wp-plugin/turn-comments-off": "^2.0"
  },
  "extra": {
    "installer-paths": {
      "web/app/mu-plugins/{$name}/": ["type:wordpress-muplugin"],
      "web/app/plugins/{$name}/": ["type:wordpress-plugin"],
      "web/app/themes/{$name}/": ["type:wordpress-theme"]
    },
    "wordpress-install-dir": "web/wp"
  },
  "config": {
    "allow-plugins": {
      "composer/installers": true,
      "roots/wordpress-core-installer": true
    }
  }
}
```

## Migrating from WPackagist

1. Remove wpackagist packages: `composer remove wpackagist-plugin/woocommerce`
2. Update your repository URL in `composer.json` (see above)
3. Add with new naming: `composer require wp-plugin/woocommerce`

Or use the [migration script](scripts/migrate-from-wpackagist.sh) to automatically update your `composer.json`:

```sh
curl -sO https://raw.githubusercontent.com/roots/wp-packages/main/scripts/migrate-from-wpackagist.sh && bash migrate-from-wpackagist.sh
```

## Community

- Join us on Discord by [sponsoring us on GitHub](https://github.com/sponsors/roots)
- Join us on [Roots Discourse](https://discourse.roots.io/)
- Follow [@rootswp on Twitter](https://twitter.com/rootswp)
- Follow the [Roots Blog](https://roots.io/blog/)
- Subscribe to the [Roots Newsletter](https://roots.io/subscribe/)
