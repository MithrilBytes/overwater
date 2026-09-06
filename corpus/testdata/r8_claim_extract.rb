# From akitaonrails/frank_investigator,
# app/services/analyzers/claim_extractor.rb, with llm_chat and primary_model
# from app/services/analyzers/llm_helpers.rb and ProviderConfig.chat from
# app/services/llm/provider_config.rb, which is where the repository builds
# the RubyLLM chat. The model list comes from config/application.rb; the
# openrouter default names three models, so only the openai default,
# gpt-5-mini, which is also the primary_model fallback, is kept. The
# heuristic fallback that runs when the LLM is unavailable is dropped.

require "set"

module Llm
  module ProviderConfig
    extend self

    def provider
      Rails.configuration.x.frank_investigator.llm_provider.to_s.presence || "openrouter"
    end

    def models
      Array(Rails.configuration.x.frank_investigator.llm_models).reject(&:blank?)
    end

    def available?
      defined?(RubyLLM) && ENV[api_key_env].present? && models.any?
    end

    def api_key_env
      case provider
      when "openai"
        "OPENAI_API_KEY"
      else
        "OPENROUTER_API_KEY"
      end
    end

    def chat(model:)
      RubyLLM.chat(
        model:,
        provider: provider.to_sym,
        assume_model_exists: provider.present?
      )
    end
  end
end

module Analyzers
  # Shared LLM interaction helpers for all analyzers.
  # Include in any analyzer that makes LLM calls via the configured LLM provider.
  module LlmHelpers
    private

    def create_interaction(model, prompt, fingerprint)
      LlmInteraction.create!(
        investigation: @investigation,
        interaction_type: interaction_type_name,
        model_id: model,
        prompt_text: prompt,
        evidence_packet_fingerprint: fingerprint,
        status: :pending
      )
    rescue StandardError => e
      Rails.logger.warn("Failed to create #{interaction_type_name} interaction: #{e.message}")
      nil
    end

    def complete_interaction(interaction, response, payload, elapsed_ms)
      return unless interaction
      interaction.update!(
        response_text: response.content.to_s,
        response_json: payload,
        status: :completed,
        latency_ms: elapsed_ms,
        prompt_tokens: response.respond_to?(:input_tokens) ? response.input_tokens : nil,
        completion_tokens: response.respond_to?(:output_tokens) ? response.output_tokens : nil
      )
    rescue StandardError => e
      Rails.logger.warn("Failed to update #{interaction_type_name} interaction: #{e.message}")
    end

    def fail_interaction(interaction, error)
      return unless interaction
      interaction.update!(status: :failed, error_class: error.class.name, error_message: error.message.truncate(500))
    rescue StandardError
      nil
    end

    def llm_available?
      Llm::ProviderConfig.available?
    end

    def primary_model
      Llm::ProviderConfig.models.first || "gpt-5-mini"
    end

    def llm_chat(model: primary_model)
      Llm::ProviderConfig.chat(model:)
    end

    def unwrap_json(content)
      text = content.to_s.strip
      text = text.sub(/\A```(?:json)?\s*\n?/, "").sub(/\n?\s*```\z/, "") if text.start_with?("```")
      text
    end
  end
end

module Analyzers
  class ClaimExtractor
    include LlmHelpers

    Result = Struct.new(:canonical_text, :surface_text, :role, :checkability_status, :importance_score, :canonical_form, :semantic_key, keyword_init: true)

    MAX_LLM_INPUT_LENGTH = 3000

    def self.call(article, investigation: nil)
      new(article, investigation:).call
    end

    def initialize(article, investigation: nil)
      @article = article
      @investigation = investigation
    end

    def call
      llm_claims = extract_with_llm
      llm_claims.uniq { |result| ClaimFingerprint.call(result.canonical_text) }
    end

    private

    def extract_with_llm
      return [] unless llm_available?

      body_sample = @article.body_text.to_s.truncate(MAX_LLM_INPUT_LENGTH)
      return [] if body_sample.length < 100

      prompt = build_llm_prompt(body_sample)
      packet_fingerprint = Digest::SHA256.hexdigest(prompt)

      interaction = record_interaction(prompt, packet_fingerprint)
      start_time = Process.clock_gettime(Process::CLOCK_MONOTONIC)

      response = llm_chat(model: extraction_model)
        .with_instructions(EXTRACTION_SYSTEM_PROMPT)
        .with_schema(extraction_schema)
        .ask(prompt)

      elapsed_ms = ((Process.clock_gettime(Process::CLOCK_MONOTONIC) - start_time) * 1000).to_i
      raise "Empty LLM response" if response.content.blank?
      payload = response.content.is_a?(Hash) ? response.content : JSON.parse(unwrap_json(response.content))
      complete_interaction(interaction, response, payload, elapsed_ms)

      parse_llm_claims(payload)
    rescue StandardError => e
      fail_interaction(interaction, e) if interaction
      Rails.logger.warn("LLM claim extraction failed: #{e.message}")
      []
    end

    EXTRACTION_SYSTEM_PROMPT = <<~PROMPT.freeze
      You are a fact-checking claim extractor. Given a news article, identify ONLY the core newsworthy factual claims.

      EXTRACT only claims that are:
      - Verifiable against official records, data, or documents
      - Central to the article's news value (not background or filler)
      - Specific enough to check (names, dates, numbers, official actions)

      DO NOT extract:
      - Opinions, rhetoric, or editorial commentary
      - Generic background context ("Brazil is the largest country in South America")
      - Website UI text, navigation, cookie notices, social share prompts
      - Author bylines, publication dates, or metadata
      - Vague or hedged statements ("some analysts believe")
      - Duplicate or near-duplicate claims (pick the most specific version)

      Aim for 3-8 high-quality claims per article. Fewer precise claims are better than many vague ones.

      For each claim, provide:
      - text: the claim as stated in the article
      - canonical_form: the claim rewritten as a clear Subject-Verb-Object sentence with proper nouns,
        ISO dates (2025-Q1, 2025-03), percentages as "X%", no hedging or attribution
      - semantic_key: a lowercase hyphenated key like "brazil-gdp-growth-3.1pct-2025-q1" (max 80 chars)
      - importance: high, medium, or low
      - checkability: checkable, not_checkable, or ambiguous
      Return only strict JSON matching the schema.
    PROMPT

    def build_llm_prompt(body_sample)
      {
        title: @article.title,
        body: body_sample,
        host: @article.host
      }.to_json
    end

    def extraction_schema
      {
        name: "claim_extraction",
        schema: {
          type: "object",
          additionalProperties: false,
          properties: {
            claims: {
              type: "array",
              items: {
                type: "object",
                additionalProperties: false,
                properties: {
                  text: { type: "string" },
                  canonical_form: { type: "string" },
                  semantic_key: { type: "string" },
                  importance: { type: "string", enum: %w[high medium low] },
                  checkability: { type: "string", enum: %w[checkable not_checkable ambiguous] }
                },
                required: %w[text canonical_form semantic_key importance checkability]
              }
            }
          },
          required: %w[claims]
        }
      }
    end

    def parse_llm_claims(payload)
      Array(payload["claims"]).filter_map do |claim_data|
        text = claim_data["text"].to_s.squish
        next if text.blank? || text.length < 30
        next if ClaimNoiseFilter.noise?(text)

        importance = case claim_data["importance"]
        when "high" then 0.95
        when "medium" then 0.75
        else 0.55
        end

        checkability = claim_data["checkability"].to_s
        checkability = "pending" unless %w[checkable not_checkable ambiguous].include?(checkability)

        Result.new(
          canonical_text: text,
          surface_text: text,
          role: :body,
          checkability_status: checkability.to_sym,
          importance_score: importance,
          canonical_form: claim_data["canonical_form"].to_s.squish.presence,
          semantic_key: claim_data["semantic_key"].to_s.downcase.gsub(/[^a-z0-9\-]/, "-").squeeze("-").truncate(80, omission: "").presence
        )
      end
    end

    def interaction_type_name
      :claim_decomposition
    end

    def extraction_model
      primary_model
    end

    def record_interaction(prompt, fingerprint)
      return nil unless @investigation
      create_interaction(extraction_model, prompt, fingerprint)
    end
  end
end
