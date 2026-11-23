`playback` owns the queue snapshot a user resumes playback from.

It owns the table `playback_queue_state`. Only `playback` writes it. Other modules may read it.
