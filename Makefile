.PHONY: build-image deploy grant-deploy-access

PROJECT_ID ?= yukan-472703
REGION ?= asia-northeast1
SERVICE ?= discord-yukan
REPOSITORY ?= yukan-bot
IMAGE_NAME ?= discord-yukan
DISCORD_BOT_TOKEN_SECRET ?= discord-bot-token:latest
GEMINI_API_KEY_SECRET ?= gemini-api-key:latest
DEPLOY_MEMBER ?=
RUN_SERVICE_ACCOUNT ?= 541943960471-compute@developer.gserviceaccount.com
IMAGE_URI := $(REGION)-docker.pkg.dev/$(PROJECT_ID)/$(REPOSITORY)/$(IMAGE_NAME)

build-image:
	gcloud builds submit --tag $(IMAGE_URI)

deploy: build-image
	gcloud run deploy $(SERVICE) \
		--image $(IMAGE_URI) \
		--platform managed \
		--region $(REGION) \
		--allow-unauthenticated \
		--remove-env-vars DISCORD_BOT_TOKEN,GEMINI_API_KEY \
		--set-secrets DISCORD_BOT_TOKEN=$(DISCORD_BOT_TOKEN_SECRET),GEMINI_API_KEY=$(GEMINI_API_KEY_SECRET)

grant-deploy-access:
ifndef DEPLOY_MEMBER
	$(error DEPLOY_MEMBER is required. Example: make grant-deploy-access DEPLOY_MEMBER=user:someone@example.com)
endif
	gcloud projects add-iam-policy-binding $(PROJECT_ID) \
		--member="$(DEPLOY_MEMBER)" \
		--role="roles/cloudbuild.builds.builder"
	gcloud projects add-iam-policy-binding $(PROJECT_ID) \
		--member="$(DEPLOY_MEMBER)" \
		--role="roles/artifactregistry.writer"
	gcloud projects add-iam-policy-binding $(PROJECT_ID) \
		--member="$(DEPLOY_MEMBER)" \
		--role="roles/run.developer"
	gcloud projects add-iam-policy-binding $(PROJECT_ID) \
		--member="$(DEPLOY_MEMBER)" \
		--role="roles/secretmanager.secretAccessor"
	gcloud iam service-accounts add-iam-policy-binding $(RUN_SERVICE_ACCOUNT) \
		--project=$(PROJECT_ID) \
		--member="$(DEPLOY_MEMBER)" \
		--role="roles/iam.serviceAccountUser"
