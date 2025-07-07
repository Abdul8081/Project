This is the implementation of the cuckoo filter in the mgpusimulator, which has been builted over the akita framework.
In this, when request is coming from the tlb, it is going to have expensive search through the gmmu and if not found then it is going to the iommu, at this place, I have implemented cuckoofilter 
which will work for each request coming from the tlb, check if that if present in the cuckoo filter then it will allow the request to go inside the gmmu for getting translartion 
request, and if particular request is not found in the cuckoo filter then it send directly that request to the iommu, to save the expenseve seach latency into the gmmu. 
