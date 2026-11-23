`playback` owns the queue snapshot a user resumes playback from.

It owns the tables `playback_queue_state`. Only `playback` writes them. Other modules may read them.
