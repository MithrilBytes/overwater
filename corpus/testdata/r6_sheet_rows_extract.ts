// From harishdeivanayagam/rowfill, src/core/extractTable.ts. The model
// literal is the repository's own. The row schema is built at run time from
// the sheet's user-defined columns, each column's instruction becoming that
// field's description. The credit check and the completion emails around
// the loop are dropped; the call and the row writes are as written.

import { logger } from "@/lib/logger"
import { prisma } from "@/lib/prisma"
import { OpenAI } from "openai"
import { zodResponseFormat } from "openai/helpers/zod"
import { z } from "zod"

export async function extractTableToSheet(sheetId: string) {
    const sheet = await prisma.sheet.findFirstOrThrow({
        where: {
            id: sheetId,
            extractInProgress: true
        },
        include: {
            sheetSources: true,
            createdBy: true
        }
    })

    if (!sheet) {
        throw new Error("Sheet not found");
    }

    if (sheet.sheetSources.length !== 1) {
        throw new Error("Sheet must have exactly one source");
    }

    try {

        const columns = await prisma.sheetColumn.findMany({
            where: {
                sheetId: sheetId
            }
        })

        // Delete all rows in the sheet
        await prisma.extractedSheetRow.deleteMany({
            where: {
                sheetId: sheetId
            }
        })

        const openai = new OpenAI({
            apiKey: process.env.OPENAI_API_KEY
        })

        const indexedSources = await prisma.indexedSource.findMany({
            where: {
                sourceId: sheet.sheetSources[0].sourceId
            }
        })

        const rowSchema = z.object(
            Object.fromEntries(
                columns.map((column) => [
                    column.name,
                    z.string().optional().describe(column.instruction)
                ])
            )
        )

        for (let indexedSource of indexedSources) {
            const response = await openai.beta.chat.completions.parse({
                model: "gpt-4o-mini",
                messages: [
                    {
                        role: "system",
                        content: "You need to extract data from a tables or charts into rows of data"
                    },
                    {
                        role: "user",
                        content: indexedSource.referenceText || ""
                    }
                ],
                temperature: 0,
                response_format: zodResponseFormat(
                    z.object({
                        rows: z.array(rowSchema)
                    }),
                    "rows"
                )
            })

            const output = response.choices[0].message.parsed

            let rowCount = 1

            if (output) {

                for (let row of output.rows) {

                    for (let columnName in row) {
                        const foundColumn = columns.find((column) => column.name === columnName)

                        if (foundColumn) {

                            await prisma.extractedSheetRow.create({
                                data: {
                                    sheetId: sheetId,
                                    sheetColumnId: foundColumn.id,
                                    rowNumber: rowCount,
                                    organizationId: sheet.organizationId,
                                    value: row[columnName],
                                    indexedSourceId: indexedSource.id
                                }
                            })
                        }
                    }

                    rowCount += 1
                }
            }
        }

        await prisma.sheet.update({
            where: {
                id: sheetId
            },
            data: {
                extractInProgress: false
            }
        })

    } catch (err) {
        logger.error(err)

        await prisma.sheet.update({
            where: {
                id: sheetId
            },
            data: {
                extractInProgress: false
            }
        })
    }

}
