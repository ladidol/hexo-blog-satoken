package org.cuit.epoch.service.impl;

import com.alibaba.fastjson.JSON;
import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import org.cuit.epoch.entity.Page;
import org.cuit.epoch.mapper.PageMapper;
import org.cuit.epoch.service.PageService;
import org.cuit.epoch.service.RedisService;
import org.cuit.epoch.util.BeanCopyUtils;
import org.cuit.epoch.vo.PageVO;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.util.List;
import java.util.Objects;

import static org.cuit.epoch.constant.RedisPrefixConst.PAGE_COVER;

/**
 * @author: ladidol
 * @date: 2022/11/16 19:52
 * @description:
 */
@Service
public class PageServiceImpl extends ServiceImpl<PageMapper, Page> implements PageService {
    @Autowired
    private RedisService redisService;
    @Autowired
    private PageMapper pageDao;

    @Transactional(rollbackFor = Exception.class)
    @Override
    public void saveOrUpdatePage(PageVO pageVO) {
        Page page = BeanCopyUtils.copyObject(pageVO, Page.class);
        this.saveOrUpdate(page);
        // 删除缓存
        redisService.del(PAGE_COVER);
    }

    @Transactional(rollbackFor = Exception.class)
    @Override
    public void deletePage(Integer pageId) {
        pageDao.deleteById(pageId);
        // 删除缓存
        redisService.del(PAGE_COVER);
    }

    @Transactional(rollbackFor = Exception.class)
    @Override
    public List<PageVO> listPages() {
        List<PageVO> pageVOList;
        // 查找缓存信息，不存在则从mysql读取，更新缓存
        Object pageList = redisService.get(PAGE_COVER);
        if (Objects.nonNull(pageList)) {
            pageVOList = JSON.parseObject(pageList.toString(), List.class);
        } else {
            pageVOList = BeanCopyUtils.copyList(pageDao.selectList(null), PageVO.class);
            redisService.set(PAGE_COVER, JSON.toJSONString(pageVOList));
        }
        return pageVOList;
    }

}