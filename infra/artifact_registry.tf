resource "google_artifact_registry_repository" "yukan" {
  repository_id = "yukan-bot"
  format        = "DOCKER"
  description   = "Yukan bot Docker images"
}
