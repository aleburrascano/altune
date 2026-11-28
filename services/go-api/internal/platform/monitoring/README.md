`monitoring` owns what operators see about the backend's health, metrics and alerts.

It owns the table `client_events`. Only `monitoring` writes it. Other modules may read it.
