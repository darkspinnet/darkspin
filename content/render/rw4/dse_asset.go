package rw4

import (
	"bufio"
	"fmt"
	"reflect"
	"sort"
)

type dseRenderAsset struct {
	MeshRef       uint32
	VertexStreams []dseVertexStream
	IndexRef      uint32
	IndexDataRef  uint32
	MaterialLinks []dseMaterialLink
}

type dseVertexStream struct {
	BufferRef      uint32
	DescriptionRef uint32
	DataRef        uint32
}

type dseMaterialLink struct {
	LinkRef   uint32
	Materials []dseMaterial
}

type dseMaterial struct {
	StateRef uint32
	Textures []dseTexture
}

type dseTexture struct {
	SamplerIndex uint32
	RasterRef    uint32
	DataRef      uint32
}

func (e *Document) writeDSERenderAssets(writer *bufio.Writer) error {
	assets, err := e.dseRenderAssets()
	if err != nil {
		return fmt.Errorf("assetGraph: %w", err)
	}
	_, err = fmt.Fprintf(writer, "\tNUMRENDERASSETS %d\n", len(assets))
	for assetIndex, asset := range assets {
		if err != nil {
			break
		}
		meshOrdinal, ordinalErr := e.directOrdinal(asset.MeshRef)
		if ordinalErr != nil {
			return fmt.Errorf("asset[%d]Mesh: %w", assetIndex, ordinalErr)
		}
		_, err = fmt.Fprintf(writer, "\t\tRENDERASSET %q\n", fmt.Sprintf("mesh%d", meshOrdinal))
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\tMESH %s\n", referenceTag(asset.MeshRef))
		}
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\tNUMVERTEXSTREAMS %d\n", len(asset.VertexStreams))
		}
		for streamIndex, stream := range asset.VertexStreams {
			if err == nil {
				_, err = fmt.Fprintf(writer, "\t\t\t\tVERTEXSTREAM %q\n", fmt.Sprintf("stream%d", streamIndex))
			}
			if err == nil {
				_, err = fmt.Fprintf(writer, "\t\t\t\t\tBUFFER? %s\n\t\t\t\t\tDESCRIPTION? %s\n\t\t\t\t\tDATA? %s\n", referenceTag(stream.BufferRef), referenceTag(stream.DescriptionRef), referenceTag(stream.DataRef))
			}
		}
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\tINDEXBUFFER? %s\n\t\t\tINDEXDATA? %s\n", referenceTag(asset.IndexRef), referenceTag(asset.IndexDataRef))
		}
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\tNUMMATERIALLINKS %d\n", len(asset.MaterialLinks))
		}
		for linkIndex, link := range asset.MaterialLinks {
			if err == nil {
				_, err = fmt.Fprintf(writer, "\t\t\t\tMATERIALLINK %q\n", fmt.Sprintf("link%d", linkIndex))
			}
			if err == nil {
				_, err = fmt.Fprintf(writer, "\t\t\t\t\tSECTION %s\n\t\t\t\t\tNUMMATERIALS %d\n", referenceTag(link.LinkRef), len(link.Materials))
			}
			for materialIndex, material := range link.Materials {
				if err == nil {
					_, err = fmt.Fprintf(writer, "\t\t\t\t\t\tMATERIAL %q\n", fmt.Sprintf("material%d", materialIndex))
				}
				if err == nil {
					_, err = fmt.Fprintf(writer, "\t\t\t\t\t\t\tSTATE %s\n\t\t\t\t\t\t\tNUMTEXTURES %d\n", referenceTag(material.StateRef), len(material.Textures))
				}
				for textureIndex, texture := range material.Textures {
					if err == nil {
						_, err = fmt.Fprintf(writer, "\t\t\t\t\t\t\t\tTEXTURE %q\n", fmt.Sprintf("texture%d", textureIndex))
					}
					if err == nil {
						_, err = fmt.Fprintf(writer, "\t\t\t\t\t\t\t\t\tSAMPLERINDEX %d\n\t\t\t\t\t\t\t\t\tRASTER? %s\n\t\t\t\t\t\t\t\t\tDATA? %s\n", texture.SamplerIndex, referenceTag(texture.RasterRef), referenceTag(texture.DataRef))
					}
				}
			}
		}
	}
	if err != nil {
		return fmt.Errorf("assetOutput: %w", err)
	}
	return nil
}

func (e *Document) dseRenderAssets() ([]dseRenderAsset, error) {
	meshOrdinals := make([]int, 0, len(e.Meshes))
	for meshOrdinal := range e.Meshes {
		meshOrdinals = append(meshOrdinals, meshOrdinal)
	}
	sort.Ints(meshOrdinals)
	assets := make([]dseRenderAsset, 0, len(meshOrdinals))
	for _, meshOrdinal := range meshOrdinals {
		mesh := e.Meshes[meshOrdinal]
		asset := dseRenderAsset{MeshRef: uint32(meshOrdinal), IndexRef: mesh.IndexBufferRef, IndexDataRef: 0x00400000}
		indexOrdinal, err := e.directOrdinal(mesh.IndexBufferRef)
		if err == nil {
			indexBuffer, isFound := e.IndexBuffers[indexOrdinal]
			if isFound {
				asset.IndexDataRef = indexBuffer.DataRef
			}
		}
		for _, bufferRef := range mesh.VertexBufferRefs {
			stream := dseVertexStream{BufferRef: bufferRef, DescriptionRef: 0x00400000, DataRef: 0x00400000}
			bufferOrdinal, ordinalErr := e.directOrdinal(bufferRef)
			if ordinalErr == nil {
				buffer, isBufferFound := e.VertexBuffers[bufferOrdinal]
				if isBufferFound {
					stream.DescriptionRef = buffer.DescriptionRef
					stream.DataRef = buffer.DataRef
				}
			}
			asset.VertexStreams = append(asset.VertexStreams, stream)
		}
		linkOrdinals := make([]int, 0, len(e.MeshStateLinks))
		for linkOrdinal, link := range e.MeshStateLinks {
			linkedMeshOrdinal, linkErr := e.directOrdinal(link.MeshRef)
			if linkErr == nil && linkedMeshOrdinal == meshOrdinal {
				linkOrdinals = append(linkOrdinals, linkOrdinal)
			}
		}
		sort.Ints(linkOrdinals)
		for _, linkOrdinal := range linkOrdinals {
			link := e.MeshStateLinks[linkOrdinal]
			materialLink := dseMaterialLink{LinkRef: uint32(linkOrdinal)}
			for _, stateRef := range link.CompiledStateRefs {
				stateOrdinal, stateErr := e.directOrdinal(stateRef)
				material := dseMaterial{StateRef: stateRef}
				state, isStateFound := e.CompiledStates[stateOrdinal]
				if stateErr != nil || !isStateFound {
					materialLink.Materials = append(materialLink.Materials, material)
					continue
				}
				for _, slot := range state.TextureSlots {
					texture := dseTexture{SamplerIndex: slot.SamplerIndex, RasterRef: slot.RasterRef, DataRef: 0x00400000}
					rasterOrdinal, rasterErr := e.directOrdinal(slot.RasterRef)
					if rasterErr == nil {
						raster, isRasterFound := e.Rasters[rasterOrdinal]
						if isRasterFound {
							texture.DataRef = raster.DataRef
						}
					}
					material.Textures = append(material.Textures, texture)
				}
				materialLink.Materials = append(materialLink.Materials, material)
			}
			asset.MaterialLinks = append(asset.MaterialLinks, materialLink)
		}
		assets = append(assets, asset)
	}
	return assets, nil
}

func readDSERenderAssets(parser *rw4Parser) ([]dseRenderAsset, error) {
	assetCount, err := parser.count("NUMRENDERASSETS")
	if err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}
	assets := make([]dseRenderAsset, assetCount)
	for assetIndex := range assets {
		assetName, readErr := parser.property("RENDERASSET", 1)
		if readErr != nil {
			return nil, fmt.Errorf("asset[%d]: %w", assetIndex, readErr)
		}
		asset := dseRenderAsset{}
		asset.MeshRef, readErr = parser.reference("MESH")
		if readErr != nil {
			return nil, fmt.Errorf("asset[%d]Mesh: %w", assetIndex, readErr)
		}
		meshOrdinal := int(asset.MeshRef & 0x003FFFFF)
		if assetName[0] != fmt.Sprintf("mesh%d", meshOrdinal) {
			return nil, fmt.Errorf("asset[%d]Name: got %q, want mesh%d", assetIndex, assetName[0], meshOrdinal)
		}
		streamCount, readErr := parser.count("NUMVERTEXSTREAMS")
		if readErr != nil {
			return nil, fmt.Errorf("asset[%d]StreamCount: %w", assetIndex, readErr)
		}
		asset.VertexStreams = make([]dseVertexStream, streamCount)
		for streamIndex := range asset.VertexStreams {
			streamName, streamErr := parser.property("VERTEXSTREAM", 1)
			if streamErr != nil || streamName[0] != fmt.Sprintf("stream%d", streamIndex) {
				return nil, fmt.Errorf("asset[%d]Stream[%d]: got %v: %v", assetIndex, streamIndex, streamName, streamErr)
			}
			stream := dseVertexStream{}
			stream.BufferRef, streamErr = parser.reference("BUFFER?")
			stream.DescriptionRef, streamErr = parser.nextReference("DESCRIPTION?", streamErr)
			stream.DataRef, streamErr = parser.nextReference("DATA?", streamErr)
			if streamErr != nil {
				return nil, fmt.Errorf("asset[%d]Stream[%d]Fields: %w", assetIndex, streamIndex, streamErr)
			}
			asset.VertexStreams[streamIndex] = stream
		}
		asset.IndexRef, readErr = parser.reference("INDEXBUFFER?")
		asset.IndexDataRef, readErr = parser.nextReference("INDEXDATA?", readErr)
		linkCount, readErr := parser.nextCount("NUMMATERIALLINKS", readErr)
		if readErr != nil {
			return nil, fmt.Errorf("asset[%d]Index: %w", assetIndex, readErr)
		}
		asset.MaterialLinks = make([]dseMaterialLink, linkCount)
		for linkIndex := range asset.MaterialLinks {
			linkName, linkErr := parser.property("MATERIALLINK", 1)
			if linkErr != nil || linkName[0] != fmt.Sprintf("link%d", linkIndex) {
				return nil, fmt.Errorf("asset[%d]Link[%d]: got %v: %v", assetIndex, linkIndex, linkName, linkErr)
			}
			link := dseMaterialLink{}
			link.LinkRef, linkErr = parser.reference("SECTION")
			materialCount, linkErr := parser.nextCount("NUMMATERIALS", linkErr)
			if linkErr != nil {
				return nil, fmt.Errorf("asset[%d]Link[%d]Fields: %w", assetIndex, linkIndex, linkErr)
			}
			link.Materials = make([]dseMaterial, materialCount)
			for materialIndex := range link.Materials {
				materialName, materialErr := parser.property("MATERIAL", 1)
				if materialErr != nil || materialName[0] != fmt.Sprintf("material%d", materialIndex) {
					return nil, fmt.Errorf("asset[%d]Link[%d]Material[%d]: got %v: %v", assetIndex, linkIndex, materialIndex, materialName, materialErr)
				}
				material := dseMaterial{}
				material.StateRef, materialErr = parser.reference("STATE")
				textureCount, materialErr := parser.nextCount("NUMTEXTURES", materialErr)
				if materialErr != nil {
					return nil, fmt.Errorf("asset[%d]Link[%d]Material[%d]Fields: %w", assetIndex, linkIndex, materialIndex, materialErr)
				}
				material.Textures = make([]dseTexture, textureCount)
				for textureIndex := range material.Textures {
					textureName, textureErr := parser.property("TEXTURE", 1)
					if textureErr != nil || textureName[0] != fmt.Sprintf("texture%d", textureIndex) {
						return nil, fmt.Errorf("asset[%d]Link[%d]Material[%d]Texture[%d]: got %v: %v", assetIndex, linkIndex, materialIndex, textureIndex, textureName, textureErr)
					}
					texture := dseTexture{}
					texture.SamplerIndex, textureErr = parser.uint32("SAMPLERINDEX")
					texture.RasterRef, textureErr = parser.nextReference("RASTER?", textureErr)
					texture.DataRef, textureErr = parser.nextReference("DATA?", textureErr)
					if textureErr != nil {
						return nil, fmt.Errorf("asset[%d]Link[%d]Material[%d]Texture[%d]Fields: %w", assetIndex, linkIndex, materialIndex, textureIndex, textureErr)
					}
					material.Textures[textureIndex] = texture
				}
				link.Materials[materialIndex] = material
			}
			asset.MaterialLinks[linkIndex] = link
		}
		assets[assetIndex] = asset
	}
	return assets, nil
}

func (e *Document) validateDSERenderAssets(actualAssets []dseRenderAsset) error {
	expectedAssets, err := e.dseRenderAssets()
	if err != nil {
		return fmt.Errorf("expected: %w", err)
	}
	normalizeDSERenderAssets(actualAssets)
	normalizeDSERenderAssets(expectedAssets)
	if !reflect.DeepEqual(actualAssets, expectedAssets) {
		return fmt.Errorf("graphMismatch: got %#v, want %#v", actualAssets, expectedAssets)
	}
	return nil
}

func normalizeDSERenderAssets(assets []dseRenderAsset) {
	for assetIndex := range assets {
		asset := &assets[assetIndex]
		asset.MeshRef = normalizeDSEReference(asset.MeshRef)
		asset.IndexRef = normalizeDSEReference(asset.IndexRef)
		asset.IndexDataRef = normalizeDSEReference(asset.IndexDataRef)
		if len(asset.VertexStreams) == 0 {
			asset.VertexStreams = nil
		}
		for streamIndex := range asset.VertexStreams {
			stream := &asset.VertexStreams[streamIndex]
			stream.BufferRef = normalizeDSEReference(stream.BufferRef)
			stream.DescriptionRef = normalizeDSEReference(stream.DescriptionRef)
			stream.DataRef = normalizeDSEReference(stream.DataRef)
		}
		if len(asset.MaterialLinks) == 0 {
			asset.MaterialLinks = nil
		}
		for linkIndex := range asset.MaterialLinks {
			link := &asset.MaterialLinks[linkIndex]
			link.LinkRef = normalizeDSEReference(link.LinkRef)
			if len(link.Materials) == 0 {
				link.Materials = nil
			}
			for materialIndex := range link.Materials {
				material := &link.Materials[materialIndex]
				material.StateRef = normalizeDSEReference(material.StateRef)
				if len(material.Textures) == 0 {
					material.Textures = nil
				}
				for textureIndex := range material.Textures {
					texture := &material.Textures[textureIndex]
					texture.RasterRef = normalizeDSEReference(texture.RasterRef)
					texture.DataRef = normalizeDSEReference(texture.DataRef)
				}
			}
		}
	}
}

func normalizeDSEReference(reference uint32) uint32 {
	if reference>>22 == 1 || reference == ^uint32(0) {
		return 0x00400000
	}
	return reference
}
