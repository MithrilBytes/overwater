# From we-promise/sure, app/models/provider/openai/auto_categorizer.rb, with
# DEFAULT_MODEL from app/models/provider/openai.rb, which is where the
# repository keeps it: the model arrives as a keyword argument from the
# provider and falls back to that constant. The chat completions path for
# custom providers, with its JSON mode fallbacks, is dropped so one model
# string is left, and the fuzzy category matching after the call is trimmed.

class Provider::Openai
  DEFAULT_MODEL = "gpt-4.1".freeze
end

class Provider::Openai::AutoCategorizer
  include Provider::Openai::Concerns::UsageRecorder

  attr_reader :client, :model, :transactions, :user_categories, :langfuse_trace, :family

  def initialize(client, model: "", transactions: [], user_categories: [], langfuse_trace: nil, family: nil)
    @client = client
    @model = model
    @transactions = transactions
    @user_categories = user_categories
    @langfuse_trace = langfuse_trace
    @family = family
  end

  def auto_categorize
    auto_categorize_openai_native
  end

  # Detailed instructions for larger models like GPT-4
  def instructions
    <<~INSTRUCTIONS.strip_heredoc
      You are an assistant to a consumer personal finance app.  You will be provided a list
      of the user's transactions and a list of the user's categories.  Your job is to auto-categorize
      each transaction.

      Closely follow ALL the rules below while auto-categorizing:

      - Return 1 result per transaction
      - Correlate each transaction by ID (transaction_id)
      - Attempt to match the most specific category possible (i.e. subcategory over parent category)
      - Any category can be used for any transaction regardless of whether the transaction is income or expense
      - If you don't know the category, return "null"
        - You should always favor "null" over false positives
        - Be slightly pessimistic.  Only match a category if you're 60%+ confident it is the correct one.
      - Each transaction has varying metadata that can be used to determine the category
        - Note: "hint" comes from 3rd party aggregators and typically represents a category name that
          may or may not match any of the user-supplied categories
    INSTRUCTIONS
  end

  private

    def auto_categorize_openai_native
      span = langfuse_trace&.span(name: "auto_categorize_api_call", input: {
        model: model.presence || Provider::Openai::DEFAULT_MODEL,
        transactions: transactions,
        user_categories: user_categories
      })

      response = client.responses.create(parameters: {
        model: model.presence || Provider::Openai::DEFAULT_MODEL,
        input: [ { role: "developer", content: developer_message } ],
        text: {
          format: {
            type: "json_schema",
            name: "auto_categorize_personal_finance_transactions",
            strict: true,
            schema: json_schema
          }
        },
        instructions: instructions
      })
      Rails.logger.info("Tokens used to auto-categorize transactions: #{response.dig("usage", "total_tokens")}")

      categorizations = extract_categorizations_native(response)
      result = build_response(categorizations)

      record_usage(
        model.presence || Provider::Openai::DEFAULT_MODEL,
        response.dig("usage"),
        operation: "auto_categorize",
        metadata: {
          transaction_count: transactions.size,
          category_count: user_categories.size
        }
      )

      span&.end(output: result.map(&:to_h), usage: response.dig("usage"))
      result
    rescue => e
      span&.end(output: { error: e.message }, level: "ERROR")
      raise
    end

    AutoCategorization = Provider::LlmConcept::AutoCategorization

    def build_response(categorizations)
      categorizations.map do |categorization|
        AutoCategorization.new(
          transaction_id: categorization.dig("transaction_id"),
          category_name: normalize_category_name(categorization.dig("category_name")),
        )
      end
    end

    def normalize_category_name(category_name)
      # Convert to string to handle non-string LLM outputs (numbers, booleans, etc.)
      normalized = category_name.to_s.strip
      return nil if normalized.empty? || normalized == "null" || normalized.downcase == "null"

      # Try exact match first
      exact_match = user_categories.find { |c| c[:name] == normalized }
      return exact_match[:name] if exact_match

      # Return normalized string if no match found (will be treated as uncategorized)
      normalized
    end

    def extract_categorizations_native(response)
      # Find the message output (not reasoning output)
      message_output = response["output"]&.find { |o| o["type"] == "message" }
      raw = message_output&.dig("content", 0, "text")

      raise Provider::Openai::Error, "No message content found in response" if raw.nil?

      JSON.parse(raw).dig("categorizations")
    rescue JSON::ParserError => e
      raise Provider::Openai::Error, "Invalid JSON in native categorization: #{e.message}"
    end

    def json_schema
      {
        type: "object",
        properties: {
          categorizations: {
            type: "array",
            description: "An array of auto-categorizations for each transaction",
            items: {
              type: "object",
              properties: {
                transaction_id: {
                  type: "string",
                  description: "The internal ID of the original transaction",
                  enum: transactions.map { |t| t[:id] }
                },
                category_name: {
                  type: "string",
                  description: "The matched category name of the transaction, or null if no match",
                  enum: [ *user_categories.map { |c| c[:name] }, "null" ]
                }
              },
              required: [ "transaction_id", "category_name" ],
              additionalProperties: false
            }
          }
        },
        required: [ "categorizations" ],
        additionalProperties: false
      }
    end

    def developer_message
      <<~MESSAGE.strip_heredoc
        Here are the user's available categories in JSON format:

        ```json
        #{user_categories.to_json}
        ```

        Use the available categories to auto-categorize the following transactions:

        ```json
        #{transactions.to_json}
        ```
      MESSAGE
    end
end
