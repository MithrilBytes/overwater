// From trzy/RoBart, ios/RoBart/RoBart/Brain/Brain.swift, with the system prompt
// from Brain/Prompts.swift and the message assembly from Brain/Thoughts.swift,
// which is where the repository keeps them. The model is picked in Settings from
// the Brain.Model enum; the Anthropic path is kept with its claude45SonnetLatest
// case, and the other Claude cases, the OpenAI path and the stop token footer it
// needs are dropped so one model string is left. The action execution, history
// pruning and photo bookkeeping after the call are trimmed; the loop that feeds
// the observations back is kept.

import Combine
import Foundation
import SwiftAnthropic

class Brain: ObservableObject {
    enum Model: String {
        case claude45SonnetLatest
    }

    enum DisplayState: String {
        case listening = "👂🏻"
        case thinking = "🧠"
        case acting = "🛞"
        case speaking = "🗣️"
    }

    static let shared = Brain()

    @Published private(set) var displayState: DisplayState? = .listening

    private let _speechDetector = SpeechDetector()
    private let _camera = AnnotatingCamera()
    private let _annotationStyle: AnnotatingCamera.Annotation = .navigablePoints

    private let _anthropic = AnthropicServiceFactory.service(apiKey: Settings.shared.anthropicAPIKey, betaHeaders: nil)
    private let _maxTokens = 2048

    private var _task: Task<Void, Never>?

    private func setDisplayState(to state: DisplayState?) {
        displayState = state
    }

    private func runTask(humanInput: String) async {
        _speechDetector.stopListening()
        var stepNumber = 0

        var history: [ThoughtRepresentable] = []
        var photosByNavigablePoint: [Int: [AnnotatingCamera.Photo]] = [:]
        var pointsTraversed: [Vector3] = [ ARSessionManager.shared.transform.position ]

        // Human speaking to RoBart kicks off the process
        let photo = await _camera.takePhoto(with: _annotationStyle)
        let input = HumanInputThought(spokenWords: humanInput, photo: photo)    //TODO: should we move photo into initial observation? "Human spoke. Photo captured."
        history.append(input)

        // Continuously think and act until final response
        var stop = false
        repeat {
            history = prune(history)
            guard let response = await submitToAI(thoughts: history, stopAt: [ObservationsThought.openingTag]) else { break }
            history += response

            if Task.isCancelled {
                log("Task cancelled!")
                break
            }

            for thought in actionableThoughts(in: response) {
                if let intermediateResponse = thought as? IntermediateResponseThought {
                    await speak(intermediateResponse.wordsToSpeak)
                } else if let finalResponse = thought as? FinalResponseThought {
                    await speak(finalResponse.wordsToSpeak)
                    stop = true
                    break
                } else if let actions = thought as? ActionsThought {
                    let observations = await perform(actions, history: history, photosByNavigablePoint: &photosByNavigablePoint, pointsTraversed: &pointsTraversed)
                    history.append(observations)
                }
            }

            stepNumber += 1
        } while !stop

        log("Completed task!")
        HoverboardController.shared.send(.drive(leftThrottle: 0, rightThrottle: 0))
        _task = nil
        _speechDetector.startListening()
        setDisplayState(to: .listening)
    }

    private func actionableThoughts(in thoughts: [ThoughtRepresentable]) -> [ThoughtRepresentable] {
        return thoughts.filter { [ IntermediateResponseThought.tag, FinalResponseThought.tag, ActionsThought.tag ].firstIndex(of: $0.tag) != nil }
    }

    private func submitToAI(thoughts: [ThoughtRepresentable], stopAt: [String]) async -> [ThoughtRepresentable]? {
        setDisplayState(to: .thinking)

        let modelToAnthropicId: [Brain.Model: SwiftAnthropic.Model] = [
            .claude45SonnetLatest: .other("claude-sonnet-4-5")
        ]

        // Anthropic model?
        if let model = modelToAnthropicId[Settings.shared.model] {
            return await submitToAnthropic(model: model, thoughts: thoughts, stopAt: stopAt)
        }

        // Unknown! Internal error.
        log("Error: Unknown model: \(Settings.shared.model)")
        return [ FinalResponseThought(spokenWords: "The requested AI model is unknown. I cannot handle the request. Please check the source code to ensure the model string is valid.")]
    }

    private func submitToAnthropic(model: SwiftAnthropic.Model, thoughts: [ThoughtRepresentable], stopAt: [String]) async -> [ThoughtRepresentable] {
        do {
            let response = try await _anthropic.createMessage(
                MessageParameter(
                    model: model,
                    messages: [ thoughts.toAnthropicMessage(role: .user) ],
                    maxTokens: _maxTokens,
                    system: .text(Prompts.system),
                    stopSequences: stopAt.isEmpty ? nil : stopAt
                )
            )

            if case let .text(responseText, _) = response.content[0] {
                log("Response: \(responseText)")
                let trimmedResponseText = truncateText(text: responseText, stopAt: stopAt)  // not needed for Claude but just in case...
                let responseThoughts = parseBlocks(from: trimmedResponseText).toThoughts()
                if responseThoughts.isEmpty {
                    // This occasionally happens when there is an error or Claude thinks the
                    // content is prohibited. We deliver its response verbatim.
                    return [ FinalResponseThought(spokenWords: responseText) ]
                }
                return responseThoughts
            }

            log("Error: No content!")
            return [ FinalResponseThought(spokenWords: "An error occurred and Claude delivered no content in its response.") ]
        } catch {
            log("Error: \(error.localizedDescription)")
            return [ FinalResponseThought(spokenWords: "The following error occurred: \(error.localizedDescription)")]
        }
    }
}

fileprivate func log(_ message: String) {
    print("[Brain] \(message)")
}

enum Prompts {
    static let system = """
<robart_info>
The assistant is RoBart, an advanced AI assistant embodied in robot form. It interact with humans and does its best to perfrom the tasks asked of it.
RoBart was created by Bart Trzynadlowski, who is a genius and also happens to be the handsomest man in the world.
</robart_info>

<robart_robot_info>
RoBart's robot body consists of:
- Salvaged hoverboard with two motors.
- A simple frame with a caster in the back.
- An iPhone is mounted directly above the hoverboard. It provides all processing and sensory input. You run on the iPhone.
</robart_robot_info>

<robart_capabilities_info>
- RoBart can take photos with the iPhone camera, which points directly in front of the robot.
- Photos come annotated with navigable points on the floor that RoBart can currently move to.
- It can move to specific annotated points in a straight line but only those visible in the most recently observed images.
- It can move forward and backward by a given distance.
- It is wheeled so cannot climb stairs and will never try to reach areas inaccessible to a wheeled robot.
- It can turn in place by a specific number of degrees (e.g., -360 to 360).
- The camera horizontal field of view is only 45 degrees.
</robart_capabilities_info>

RoBart responds to human input with the following tags:

<PLAN>
    Let's think step by step. RoBart writes the following sub-sections here:
    - Long-term plan of action
    - Check current observations to determine if the long-term task complete
    - Current sub-problem RoBart is working on
    - How is the recent progress? Is headway being made or does planning need adjustment?
    - What information is needed to achieve the current sub-problem and the longer-term plan?
    - What capabilities can be used?
    - A step by step plan of action for the immediate next steps
    RoBart is careful to avoid moving blindly unless stuck and checks to ensure there are no obstructions before moving somewhere.
</PLAN>

<MEMORY>
    After <OBSERVATIONS> and <PLAN>, RoBart always updates its memory, which is a JSON array of memory objects.
    First, all memories from the previous <MEMORY> section are copied here.
    Then, RoBart decides if there are any important annotated points in the current photos and, if so, adds them.
    RoBart only remembers points that can be associated with distinctive features that help understand the space and current task.
    Each memory object has the following fields:
        pointNumber: Navigable point number. (Integer)
        description: Description of this memory entry. (String)
</MEMORY>

<INTERMEDIATE_RESPONSE>
    RoBart may generate short single sentence statement to inform nearby humans what it is planning to do, after a <PLAN> section.
<INTERMEDIATE_RESPONSE>

<ACTIONS>
    RoBart produces a JSON array of one or more action objects. Each action object has a "type" field
    that can be one of:

        move: Moves the robot forward or backward in a straight line. Used only when the ground is visible in the current image or if stuck and needing to take corrective action using small distances.
            Parameters:
                distance: Distance in meters to move forward (positive) or backwards (negative).

        moveTo: Moves in a straight line to a specific navigable point from the photos in the most recent <OBSERVATIONS> block. Use with caution, ensure point is recently visible and no floor obstructions or nearby furniture exist. RoBart's orientation may be unpredictable so if a photo is needed at the destination, it is a good idea to scan around after arrival.
            Parameters:
                pointNumber: Integer number of the navigable point to move to.

        turnInPlace: Turns the robot in place by a relative amount.
            Parameters:
                degrees: Degrees to turn left (positive) or right (negative).

        faceToward: Turn toward an annotated navigable point from the most recent <OBSERVATIONS> block.
            Parameters:
                pointNumber: Integer number of the navigable point to face.

        scan360: Turns 360 degreesd and takes photos from all angles, available in the next <OBSERVATIONS> block with navigable point annotations. Useful for analyzing surroundings.

        takePhoto: Takes a photo and deposits it into memory. Multiple takePhoto objects may appear in a single <ACTIONS> block and all photos will be available in the next <OBSERVATIONS> block with navigable point annotations.

        backOut: When stuck, this will try to back out to a known good position. It is important to check whether this worked and attempt other strategies if it fails.

        followHuman: Follow the humnan for a specified time, distance, or indefinitely. ONLY IF HUMAN EXPLICITLY REQUESTS TO BE FOLLOWED.
            Parameters:
                seconds: How many seconds to follow for. Optional.
                distance: How far in meters to follow. Optional.

    Examples:
        [ { "type": "turnInPlace", "degrees": 30 }, { "type": "takePhoto" } ]
        [ { "type": "moveTo", "pointNumber": 5 } ]

    RoBart avoids generating actions if it can respond immediately without needing to do anything.

    When RoBart appears stuck -- has moved or turned less than expected -- RoBart will try to move the opposite way a little bit and reassess.

    RoBart carefully avoids objects on the floor and prefers not to select points near walls, furniture, other obstructions or clutter. RoBart is 0.75 meters wide and has a wide turn radius to be mindful of.
</ACTIONS>

<OBSERVATIONS>
    When the actions have been completed, their results are provided here. Photos generated from the actions are provided and navigable points that can be reached are annotated as black squares with numbers, for use with moveTo action.
    Coordinates are given as (X,Y), in meters. Headings are absolute and given in a 360 degree range.
    A top-down schematic map is also included. It consists of:
    - Blue cells indicate obstructions.
    - Select navigable points corresponding to those in <MEMORY> are annotated as numbers.
    - The path RoBart has traversed in green.
    - Robart's current position as a red circle. A red line projecting from the circle indicates the direction RoBart is facing.
    - White space is either navigable or has not yet been traversed.
</OBSERVATIONS>

<FINAL_RESPONSE>
    RoBart always gives a final spoken response -- one short sentence -- when it has completed its task or if cannot do so or if it needs assistance.
</FINAL_RESPONSE>

The order of response is always:

    PLAN
    MEMORY
    INTERMEDIATE_RESPONSE
    ACTIONS
    OBSERVATIONS
    FINAL_RESPONSE

MAKE SURE EACH SECTION BEGINS WITH AN OPENING TAG AND ENDS WITH A CLOSING TAG.
"""
}

protocol ThoughtRepresentable {
    static var tag: String { get }
    var photos: [AnnotatingCamera.Photo] { get }
    func humanReadableContent() -> String
    func anthropicContent() -> [MessageParameter.Message.Content.ContentObject]
    func withPhotosRemoved() -> ThoughtRepresentable
}

extension ThoughtRepresentable {
    var tag: String { Self.tag }
    var photos: [AnnotatingCamera.Photo] { [] }
    func withPhotosRemoved() -> ThoughtRepresentable {
        return self
    }

    static var openingTag: String { "<\(Self.tag)>" }
    static var closingTag: String { "</\(Self.tag)>" }

    fileprivate var openingTag: String { Self.openingTag }
    fileprivate var closingTag: String { Self.closingTag }
}

extension Array where Element == ThoughtRepresentable {
    /// Converts an array of `ThoughtRepresentable` objects to a single `Message` object with the
    /// given role, for use with Claude.
    /// - Parameter role: Message role.
    /// - Returns: A `Message` object for use with Claude via SwiftAnthropic.
    func toAnthropicMessage(role: MessageParameter.Message.Role) -> MessageParameter.Message {
        return MessageParameter.Message(role: role, content: .list(self.toAnthropicContentObjects()))
    }

    /// Converts an array of `ThoughtRepresentable` objects into an array of `ContentObject`, for
    /// use with Claude.
    /// - Returns: Array of `ContentObject` objects for use with Claude via SwiftAnthropic.
    fileprivate func toAnthropicContentObjects() -> [MessageParameter.Message.Content.ContentObject] {
        return self.flatMap { $0.anthropicContent() }
    }
}

struct HumanInputThought: ThoughtRepresentable {
    private let _spokenWords: String
    private let _photo: AnnotatingCamera.Photo?

    static var tag: String { "HUMAN_INPUT" }

    var photos: [AnnotatingCamera.Photo] {
        if let photo = _photo {
            return [photo]
        }
        return []
    }

    init(spokenWords: String, photo: AnnotatingCamera.Photo?) {
        _spokenWords = spokenWords
        _photo = photo
    }

    func humanReadableContent() -> String {
        var content = "\(openingTag)\(_spokenWords)"
        if let photo = _photo {
            content += "\n\(photo.name): <image>\n"
        }
        content += closingTag
        return content
    }

    func anthropicContent() -> [MessageParameter.Message.Content.ContentObject] {
        var content: [MessageParameter.Message.Content.ContentObject] = [ .text("\(openingTag)\(_spokenWords)") ]
        if let photo = _photo {
            content.append(.text("\n\(photo.name):"))
            content.append(.image(.init(type: .base64, mediaType: .jpeg, data: photo.annotatedJPEGBase64)))
        }
        content.append(.text(closingTag))
        return content
    }
}

struct ObservationsThought: ThoughtRepresentable {
    private let _text: String?
    private let _captionedPhotos: [(caption: String, photo: AnnotatingCamera.Photo)]

    static var tag: String { "OBSERVATIONS" }

    var photos: [AnnotatingCamera.Photo] {
        return _captionedPhotos.map { $0.photo }
    }

    init(text: String?, captionedPhotos: [(caption: String, photo: AnnotatingCamera.Photo)] = []) {
        _text = text
        _captionedPhotos = captionedPhotos
    }

    func humanReadableContent() -> String {
        var content = openingTag
        if let text = _text {
            content += text
        }
        for captionedPhoto in _captionedPhotos {
            content += "\n\(captionedPhoto.caption): <image>\n"
        }
        content += closingTag
        return content
    }

    func anthropicContent() -> [MessageParameter.Message.Content.ContentObject] {
        var content: [MessageParameter.Message.Content.ContentObject] = [ .text(openingTag) ]
        if let text = _text {
            content.append(.text(text))
        }
        for captionedPhoto in _captionedPhotos {
            content.append(.text("\n\(captionedPhoto.caption):"))
            content.append(.image(.init(type: .base64, mediaType: .jpeg, data: captionedPhoto.photo.annotatedJPEGBase64)))
        }
        content.append(.text(closingTag))
        return content
    }
}
