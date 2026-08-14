# From hexlet-basics/hexlet-basics,
# legacy/app/jobs/find_related_courses_for_blog_post_job.rb, with the
# RubyLLM default_model from legacy/config/initializers/ruby_llm.rb, which
# is where the repository keeps it. The prompt is Russian in the original
# and is kept that way.

RubyLLM.configure do |config|
  config.openai_api_key = ENV.fetch("OPENAI_ACCESS_TOKEN")
  config.default_model = ENV.fetch("AI_DEFAULT_MODEL", "gpt-4.1")
  config.model_registry_class = "AiModel"
end

class FindRelatedCoursesForBlogPostJob < ApplicationJob
  def perform(blog_post_id)
    blog_post = BlogPost.find(blog_post_id)
    landing_pages = Language::LandingPage.published.where(main: true).with_locale(blog_post.locale).includes(:language)

    instructions = <<~PROMPT
      Ты - ассистент, который помогает подобрать курсы.
      У тебя есть текст статьи блога и список курсов.
      Выбери пять курсов подходящих под тему статьи в порядке приоритета. Первый наиболее близок, последний - наименее.
      Верни результат в виде JSON-массива идентификаторов курсов (по полю `id`) отсортированный по похожести.
      Первыми должны идти наиболее близкие курсы.
    PROMPT

    languages_data = landing_pages.map do |lp|
      { id: lp.language.id, name: lp.header }
    end

    content = blog_post.content_for_plain_text
    response = RubyLLM.chat
      .with_instructions(instructions)
      .ask([
        "Текст статьи: #{content.truncate(2000)}",
        "Список курсов: #{languages_data.to_json}"
      ].join("\n\n"))

    raw_output = response.content.to_s.gsub(/\A```(?:json)?|```\z/, "").strip
    course_ids = JSON.parse(raw_output)

    return if course_ids.empty?

    blog_post.related_language_items.delete_all
    course_ids.each_with_index do |id, i|
      item = blog_post.related_language_items.build
      item.language_id = id
      item.order = i + 1
      item.save!
    end
  end
end
