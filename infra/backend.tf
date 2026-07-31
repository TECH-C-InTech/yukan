terraform {
  backend "gcs" {
    bucket = "yukan-discord-bot-tfstate"
    prefix = "yukan"
  }
}
